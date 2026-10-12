package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentAndFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOE_TEST_VALUE", "example")
	if err := os.WriteFile(filepath.Join(dir, "input.json"), []byte(`{"value":21}`), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	source := `
if (noe.env.get("NOE_TEST_VALUE") !== "example") throw new Error("environment");
if (noe.env.get("NOE_TEST_UNSET_79262") !== undefined) throw new Error("missing environment");
if (noe.cwd() !== noe.args[2]) throw new Error("cwd");
const data = JSON.parse(await noe.readTextFile(noe.args[0]));
data.value *= 2;
const result = await noe.writeTextFile(noe.args[1], JSON.stringify(data));
if (result !== undefined) throw new Error("write return value");
console.log(data.value);`
	code, err := Execute(context.Background(), Source{filepath.Join(dir, "main.js"), source}, Options{Stdout: &stdout, Cwd: dir, Args: []string{"input.json", "output.json", dir}})
	if code != 0 || err != nil || stdout.String() != "42\n" {
		t.Fatalf("code=%d err=%v out=%q", code, err, &stdout)
	}
	data, err := os.ReadFile(filepath.Join(dir, "output.json"))
	if err != nil || string(data) != `{"value":42}` {
		t.Fatalf("data=%q err=%v", data, err)
	}
	for _, source := range []string{`await noe.readTextFile("missing.json")`, `await noe.writeTextFile("missing-dir/output.json", "text")`} {
		code, err := Execute(context.Background(), Source{filepath.Join(dir, "error.js"), source}, Options{Cwd: dir})
		if code != 1 || err == nil {
			t.Fatalf("code=%d err=%v", code, err)
		}
	}
	code, err = Execute(context.Background(), Source{filepath.Join(dir, "drain.js"), `noe.writeTextFile("unawaited.txt", "finished");`}, Options{Cwd: dir})
	data, readErr := os.ReadFile(filepath.Join(dir, "unawaited.txt"))
	if code != 0 || err != nil || readErr != nil || string(data) != "finished" {
		t.Fatalf("code=%d err=%v data=%q readErr=%v", code, err, data, readErr)
	}
}

func TestExplicitExit(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		code         int
	}{
		{"zero", `noe.exit(0); console.log("after");`, 0},
		{"nonzero", `noe.exit(7); console.log("after");`, 7},
		{"default", `noe.exit(); console.log("after");`, 0},
		{"awaited continuation", `await Promise.resolve(); noe.exit(8); console.log("after");`, 8},
		{"unawaited continuation", `Promise.resolve().then(() => { noe.exit(9); console.log("after"); });`, 9},
		{"finally", `try { noe.exit(0); } catch(e) { console.log("caught"); } finally { console.log("finally"); }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "exit.js"), tc.source}, Options{Stdout: &stdout})
			if code != tc.code || err != nil || stdout.Len() != 0 {
				t.Fatalf("code=%d err=%v out=%q", code, err, &stdout)
			}
		})
	}
	_, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "bad-exit.js"), `noe.exit(-1)`}, Options{})
	if err == nil || !strings.Contains(err.Error(), "exit code") {
		t.Fatalf("err=%v", err)
	}
}
