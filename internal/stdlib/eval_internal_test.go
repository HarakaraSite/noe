package stdlib

import (
	"errors"
	"strings"
	"testing"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
)

// The public evaluation adapter must retain the initialization semantics that
// the upstream library obtains through its private engine callback.
func TestEvalInternalResultAndPromiseJobs(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()

	value, err := evalInternal(rt, "<noe-stdlib-init>", `
		globalThis.initialized = false;
		Promise.resolve().then(() => { globalThis.initialized = true; });
		42;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if value.Int() != 42 {
		t.Fatalf("initialization result = %s, want 42", value.String())
	}
	// Read without another Eval, which could itself drain pending jobs.
	initialized, err := rt.Get("initialized")
	if err != nil {
		t.Fatal(err)
	}
	if !initialized.Bool() {
		t.Fatal("initialization returned before its Promise jobs ran")
	}
}

func TestEvalInternalErrorSourceAndClosedRuntime(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()

	const source = "<noe-stdlib-failed-init>"
	_, err := evalInternal(rt, source, `throw new Error("initialization failed")`)
	var exception *quickjs.Error
	if !errors.As(err, &exception) || !strings.Contains(err.Error(), "initialization failed") {
		t.Fatalf("initialization error = %v, want the JavaScript exception", err)
	}
	if !strings.Contains(exception.Stack(), source) {
		t.Fatalf("initialization stack = %s, want source name", exception.Stack())
	}
	rt.Close()
	_, err = evalInternal(rt, source, "42")
	if !errors.Is(err, quickjs.ErrClosed) {
		t.Fatalf("closed runtime error = %v, want ErrClosed", err)
	}
}
