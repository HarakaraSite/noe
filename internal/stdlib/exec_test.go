package stdlib_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

// Run the test executable itself so output, exit codes, and timeouts do not
// depend on external utilities or platform-specific shell syntax.
func TestSyncCommandHelper(t *testing.T) {
	switch os.Getenv("NOE_SYNC_COMMAND_HELPER") {
	case "failure":
		fmt.Print("out 日本語\n\x00")
		fmt.Fprint(os.Stderr, "err 日本語\n\x00")
		os.Exit(23)
	case "empty":
		os.Exit(7)
	case "timeout":
		fmt.Print("before timeout")
		fmt.Fprint(os.Stderr, "timeout details")
		time.Sleep(time.Minute)
		os.Exit(0)
	case "success":
		input, _ := io.ReadAll(os.Stdin)
		cwd, _ := os.Getwd()
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{
			"input": string(input), "cwd": cwd, "value": os.Getenv("VALUE"),
		})
		fmt.Fprint(os.Stderr, "successful stderr")
		os.Exit(0)
	}
}

func syncCommandCalls(t *testing.T, file string) map[string]string {
	t.Helper()
	name, _ := json.Marshal(file)
	args := `["-test.run=^TestSyncCommandHelper$"]`
	quoted := "'" + strings.ReplaceAll(file, "'", "'\"'\"'") + "'"
	if runtime.GOOS == "windows" {
		quoted = "\"" + file + "\""
	}
	command, _ := json.Marshal(quoted + " -test.run=^TestSyncCommandHelper$")
	return map[string]string{
		"execFileSync": "cp.execFileSync(" + string(name) + ", " + args + ", options)",
		"execSync":     "cp.execSync(" + string(command) + ", options)",
	}
}

func TestSyncCommandFailureDetails(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for method, call := range syncCommandCalls(t, executable) {
		for _, tc := range []struct {
			name, mode     string
			code           int
			stdout, stderr string
			limit          int
		}{
			{name: "nonzero", mode: "failure", code: 23, stdout: "out 日本語\n\x00", stderr: "err 日本語\n\x00"},
			{name: "empty", mode: "empty", code: 7},
			{name: "capped", mode: "failure", code: 23, stdout: "out", stderr: "err", limit: 3},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				expected, _ := json.Marshal(map[string]any{"code": tc.code, "stdout": tc.stdout, "stderr": tc.stderr})
				out, errOut := run(t, stdlib.Config{Run: &stdlib.Run{
					Allow:          func(string, []string) error { return nil },
					Env:            map[string]string{"NOE_SYNC_COMMAND_HELPER": tc.mode},
					MaxOutputBytes: tc.limit,
				}}, fmt.Sprintf(`
     import("node:child_process").then(cp => {
      // Synchronous output remains text, even with encoding: "buffer".
      const options = {encoding: "buffer"};
      const want = %s;
      let caught = false;
      try { %s; } catch (e) {
       caught = true;
       if (!(e instanceof Error) || e.name !== "Error") throw Error("not an Error");
       if (e.code !== want.code || e.stdout !== want.stdout || e.stderr !== want.stderr)
        throw Error("wrong details: " + JSON.stringify(e));
       if (!e.message.endsWith(" exited with " + want.code)) throw Error("message changed: " + e.message);
      }
      if (!caught) throw Error("did not throw");
      console.log("checked");
     });
    `, expected, call))
				if out != "checked" {
					t.Fatalf("stdout=%q stderr=%q", out, errOut)
				}
			})
		}
	}
}

func TestSyncCommandSuccessCompatibility(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for method, call := range syncCommandCalls(t, executable) {
		t.Run(method, func(t *testing.T) {
			dir := t.TempDir()
			// EvalSymlinks handles platforms where the temporary directory is aliased.
			dir, err = filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			cwd, _ := json.Marshal(dir)
			out, errOut := run(t, stdlib.Config{Run: &stdlib.Run{
				Allow: func(string, []string) error { return nil },
			}}, fmt.Sprintf(`
    import("child_process").then(cp => {
     for (const encoding of [undefined, "buffer"]) {
      const options = {cwd: %s, env: {NOE_SYNC_COMMAND_HELPER:"success", VALUE:"provided"}, input:"input 日本語", encoding};
      const output = %s;
      if (typeof output !== "string") throw Error("success type changed");
      const value = JSON.parse(output);
      if (value.cwd !== options.cwd || value.value !== "provided" || value.input !== options.input)
       throw Error("options changed: " + output);
     }
     console.log("checked");
    });
   `, cwd, call))
			if out != "checked" {
				t.Fatalf("stdout=%q stderr=%q", out, errOut)
			}
		})
	}
}

func TestSyncCommandStartFailureDetails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-program")
	// execSync starts a shell successfully; a missing inner program is a shell
	// exit failure, not a launch failure. Check direct launch here.
	for _, tc := range []struct {
		name    string
		allow   func(string, []string) error
		message string
	}{
		{name: "missing", allow: func(string, []string) error { return nil }},
		{name: "refused", allow: func(string, []string) error { return errors.New("permission denied by host") }, message: "permission denied by host"},
		{name: "default", message: "running programs is not allowed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name, _ := json.Marshal(missing)
			message, _ := json.Marshal(tc.message)
			out, errOut := run(t, stdlib.Config{Run: &stdlib.Run{Allow: tc.allow}}, fmt.Sprintf(`
    import("node:child_process").then(cp => {
     let caught = false;
     try { cp.execFileSync(%s); } catch (e) {
      caught = true;
      // Match the existing async convention: no process exit code is
      // available on start failure, so code is 0, not proof of success.
      if (!(e instanceof Error) || e.code !== 0 || e.stdout !== "" || e.stderr !== "") throw Error("wrong start details");
      if (%s && e.message !== %s) throw Error("message changed");
      if (!e.message) throw Error("missing message");
     }
     if (!caught) throw Error("did not throw");
     console.log("checked");
    });
   `, name, message, message))
			if out != "checked" {
				t.Fatalf("stdout=%q stderr=%q", out, errOut)
			}
		})
	}
}

func TestSyncCommandTimeoutDetails(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A direct process avoids a shell child keeping the output pipes open after
	// its parent is killed, which is a separate process-lifetime concern.
	call := syncCommandCalls(t, executable)["execFileSync"]
	out, errOut := run(t, stdlib.Config{Run: &stdlib.Run{
		Allow:   func(string, []string) error { return nil },
		Env:     map[string]string{"NOE_SYNC_COMMAND_HELPER": "timeout"},
		Timeout: 2 * time.Second,
	}}, fmt.Sprintf(`
  import("node:child_process").then(cp => {
   const options = {};
   let caught = false;
   try { %s; } catch (e) {
    caught = true;
    if (!(e instanceof Error) || e.code !== -1 || e.stdout !== "before timeout" || e.stderr !== "timeout details")
     throw Error("wrong timeout details: " + JSON.stringify(e));
    if (!e.message.endsWith(" took longer than 2s")) throw Error("message changed");
   }
   if (!caught) throw Error("did not throw");
   console.log("checked");
  });
 `, call))
	if out != "checked" {
		t.Fatalf("stdout=%q stderr=%q", out, errOut)
	}
}
