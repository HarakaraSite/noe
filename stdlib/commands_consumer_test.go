//go:build !windows

package stdlib

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExternalCommandConsumerExplicitHelper(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source, err := os.ReadFile(filepath.Join(root, "stdlib/testdata/commands/main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "main.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	mod := fmt.Sprintf("module example.com/commandconsumer\n\ngo 1.27.2\nrequire %s v2.0.0-dev.0\nreplace %s => %q\n", publicModule, publicModule, filepath.ToSlash(root))
	mod += fmt.Sprintf("replace github.com/go-quickjs/go-quickjs => %q\n", filepath.ToSlash(filepath.Join(root, "third_party", "go-quickjs")))
	if err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := func(output string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "go", append([]string{"build", "-mod=mod", "-o", output}, args...)...)
		cmd.Dir = dir
		cmd.Env = consumerEnvironment(map[string]string{"GOWORK": "off", "CGO_ENABLED": "0", "GOFLAGS": ""})
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build consumer: %v\n%s", err, out)
		}
	}
	consumer := filepath.Join(dir, "consumer")
	build(consumer, ".")
	helperSource := `package main
import("os";"forge.harakara.site/littleisland/noe/v2/stdlib")
func main(){if code,handled:=stdlib.DispatchProcessHelper(os.Args[1:]);handled{os.Exit(code)};os.Exit(125)}`
	helperFile := filepath.Join(dir, "helper.go")
	if err = os.WriteFile(helperFile, []byte(helperSource), 0600); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "helper")
	build(helper, helperFile)
	for _, tc := range []struct {
		name, helper string
		inits        int
	}{{"direct", "", 1}, {"self-helper", consumer, 2}, {"separate-helper", helper, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			counter := filepath.Join(dir, tc.name+"-inits")
			cmd := exec.CommandContext(ctx, consumer)
			cmd.Env = consumerEnvironment(map[string]string{"NOE_TEST_INIT_FILE": counter, "NOE_TEST_SUPERVISOR": tc.helper, "NOE_TEST_SKIP_DISPATCH": ""})
			output, err := cmd.CombinedOutput()
			if err != nil || string(output) != "command-ok" {
				t.Fatalf("consumer output=%q err=%v", output, err)
			}
			counts, err := os.ReadFile(counter)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(string(counts), "init\n"); got != tc.inits {
				t.Fatalf("application init ran %d times, want %d", got, tc.inits)
			}
		})
	}
	if runtime.GOOS == "linux" {
		noe := filepath.Join(dir, "noe-helper")
		build(noe, publicModule+"/cmd/noe")
		for name, executable := range map[string]string{"separate": helper, "noe": noe} {
			t.Run(name+"-owner-death", func(t *testing.T) { checkCommandOwnerDeath(t, ctx, consumer, executable) })
		}
	}
	// Importing stdlib must never intercept private argv from the application.
	cmd := exec.CommandContext(ctx, consumer, "--noe-private-process-supervisor-v1")
	cmd.Env = consumerEnvironment(map[string]string{"NOE_TEST_SKIP_DISPATCH": "1", "NOE_TEST_INIT_FILE": ""})
	if output, err := cmd.CombinedOutput(); err != nil || string(output) != "application" {
		t.Fatalf("library intercepted application args: %q, %v", output, err)
	}
}
