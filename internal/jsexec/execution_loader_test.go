package jsexec

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// Direct Attach users can register and load sources through Bindings. The
// extracted loader must observe the same close gate as initializer cleanup
// and managed entry registration, rather than keeping its own open state.
func TestBindingsLoaderSharesCloseState(t *testing.T) {
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	bindings, err := Attach(rt, loop, Profile{})
	if err != nil {
		loop.Close()
		_ = rt.Close()
		t.Fatal(err)
	}
	defer func() {
		_ = bindings.Close()
		loop.Close()
		_ = rt.Close()
	}()

	entry := Source{Name: "[loader lifetime]", Virtual: true, Module: true}
	if err := bindings.DefineModule("owner:before", "export default 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := bindings.Prepare(entry); err != nil {
		t.Fatal(err)
	}
	if _, err := bindings.Prepare(entry); err == nil || err.Error() != "stdlib: an entry was already prepared" {
		t.Fatalf("second Prepare = %v", err)
	}
	if err := bindings.DefineModule("owner:after", "export default 2"); err != nil {
		t.Fatal(err)
	}
	source, name, err := bindings.LoadModule("owner:after", entry.Name)
	if err != nil || source != "export default 2" || name != "owner:after" {
		t.Fatalf("load module registered after Prepare = %q, %q, %v", source, name, err)
	}

	if err := bindings.Close(); err != nil {
		t.Fatal(err)
	}
	for name, operation := range map[string]func() error{
		"DefineModule": func() error { return bindings.DefineModule("owner:closed", "export default 3") },
		"Prepare": func() error {
			_, err := bindings.Prepare(entry)
			return err
		},
		"managed modules": func() error {
			_, err := bindings.installManagedModules("private:helper", "private:entry", "", nil)
			return err
		},
	} {
		if err := operation(); err == nil || err.Error() != "stdlib: bindings are closed" {
			t.Errorf("%s after Close = %v", name, err)
		}
	}
	if _, _, err := bindings.LoadModule("owner:after", entry.Name); !errors.Is(err, context.Canceled) {
		t.Errorf("LoadModule after Close = %v, want execution cancellation", err)
	}
}

// Modules run through the same dynamic-import wrapper as CLI and agent tools.
// Keep the reported upstream block-declaration regression covered here, rather
// than relying only on direct engine evaluation.
func TestModuleBlockFunctionDeclarationsAndShadowing(t *testing.T) {
	for _, tc := range []struct {
		name, code string
	}{
		{"async declaration", `try {
  async function worker() { return 7; }
  console.log(await worker());
} catch (error) { throw error; }`},
		{"shadowing", `function worker() { return 99; }
try {
  function worker() { return 7; }
  console.log(worker());
} catch (error) { throw error; }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			result, err := Execute(context.Background(), Source{
				Name: "memory:block-functions", Module: true, Virtual: true, Code: tc.code,
			}, Profile{Environment: Environment{Stdout: &output}})
			if err != nil || result.Code != 0 || output.String() != "7\n" {
				t.Fatalf("block declaration: result=%+v output=%q err=%v", result, output.String(), err)
			}
		})
	}
}
