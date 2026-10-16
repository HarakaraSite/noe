package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionStateAndRecovery(t *testing.T) {
	var stdout bytes.Buffer
	s, err := NewSession(Options{Stdout: &stdout})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct{ input, out, diagnostic string }{
		{`let count = 1`, "", ""},
		{`count += 1`, "2", ""},
		{`function twice(n) { return n*2; }`, "", ""},
		{`twice(count)`, "4", ""},
		{`const obj = {value: count}`, "", ""},
		{`obj`, "{ value: 2 }", ""},
		{`[1,2,3]`, "[ 1, 2, 3 ]", ""},
		{`console.log("hello")`, "", ""},
		{`const broken = ;`, "", "SyntaxError"},
		{`count = 7; throw new Error("failed input")`, "", "failed input"},
		{`count`, "7", ""},
		{`Promise.resolve().then(() => { throw new Error("late failure"); })`, "", "late failure"},
		{`count`, "7", ""},
	} {
		got := s.Eval(context.Background(), tc.input)
		if got.Exit || got.Output != tc.out {
			t.Fatalf("input=%s result=%+v", tc.input, got)
		}
		if tc.diagnostic == "" {
			if got.Err != nil {
				t.Fatalf("input=%s err=%v", tc.input, got.Err)
			}
		} else if got.Err == nil || !strings.Contains(Diagnostic(got.Err), tc.diagnostic) {
			t.Fatalf("input=%s err=%v", tc.input, got.Err)
		}
	}
	if stdout.String() != "hello\n" {
		t.Fatalf("stdout=%q", &stdout)
	}
}

func TestSessionFormatterExitAndMultipleDiagnostics(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result := s.Eval(context.Background(), `Promise.resolve().then(() => { throw new Error("late"); }); throw new Error("main");`)
	if result.Exit || result.Err == nil {
		t.Fatalf("result=%+v", result)
	}
	diagnostic := Diagnostic(result.Err)
	if !strings.Contains(diagnostic, "late") || !strings.Contains(diagnostic, "main") {
		t.Fatalf("diagnostic=%q", diagnostic)
	}
	result = s.Eval(context.Background(), `({ get value() { process.exit(7); } })`)
	if !result.Exit || result.Code != 7 || result.Err != nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestSessionLastResult(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "result.txt"), []byte("from I/O"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(Options{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	check := func(input, output, diagnostic string) {
		t.Helper()
		got := s.Eval(ctx, input)
		if got.Exit || got.Output != output {
			t.Fatalf("input=%q result=%+v", input, got)
		}
		if diagnostic == "" {
			if got.Err != nil {
				t.Fatalf("input=%q err=%v", input, got.Err)
			}
		} else if got.Err == nil || !strings.Contains(Diagnostic(got.Err), diagnostic) {
			t.Fatalf("input=%q err=%v, want diagnostic containing %q", input, got.Err, diagnostic)
		}
	}

	check(`_`, "", "")
	check(`Object.hasOwn(globalThis, "_")`, "true", "")
	check(`8`, "8", "")
	check(`undefined`, "", "")
	check(`_`, "8", "")
	check(`throw new Error("evaluation failed")`, "", "evaluation failed")
	check(`_`, "8", "")

	check(`const objectResult = { value: 1 }; objectResult`, "{ value: 1 }", "")
	check(`objectResult === _`, "true", "")
	check(`_ = 17`, "17", "")
	check(`_`, "17", "")

	check(`await Promise.resolve("async result")`, `'async result'`, "")
	check(`await Promise.reject(new Error("async failed"))`, "", "async failed")
	check(`_`, `'async result'`, "")
	check(`await (await import("node:fs/promises")).readFile("`+filepath.ToSlash(filepath.Join(dir, "result.txt"))+`", "utf8")`, `'from I/O'`, "")
	check(`_`, `'from I/O'`, "")

	check(`({ get value() { throw new Error("format failed") } })`, "", "format failed")
	check(`_`, `'from I/O'`, "")
}

func TestSessionIncomplete(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, source := range []string{
		`function add(a, b) {`,
		`({ value: 1`,
		`const item = { value: 1`,
		`1 + (`,
		"`hello ${name",
		`/* unfinished comment`,
		`"unfinished string`,
		`await Promise.resolve(`,
		"const pending = await Promise.resolve(\n",
	} {
		if !s.Incomplete(source) {
			t.Errorf("Incomplete(%q) = false, want true", source)
		}
	}

	for _, source := range []string{
		`function add(a, b) { return a + b; }`,
		`({ value: 1 })`,
		"const awaited = await Promise.resolve(\n 42\n)",
		`let trailing = await Promise.resolve(2); // trailing, comment`,
		`const broken = ;`,
		`if (true]`,
		`import { value } from`,
		`export { value }`,
	} {
		if s.Incomplete(source) {
			t.Errorf("Incomplete(%q) = true, want false", source)
		}
	}
	malformed := s.Eval(context.Background(), `if (true]`)
	if malformed.Exit || malformed.Err == nil {
		t.Fatalf("definite syntax error was not diagnosed: result=%+v", malformed)
	}

	if s.Incomplete(`globalThis.incompleteProbe = 1`) {
		t.Fatal("complete assignment was reported incomplete")
	}
	got := s.Eval(context.Background(), `Object.hasOwn(globalThis, "incompleteProbe")`)
	if got.Exit || got.Err != nil || got.Output != "false" {
		t.Fatalf("Incomplete executed input: result=%+v", got)
	}

	got = s.Eval(context.Background(), "const awaited = await Promise.resolve(\n 42\n)")
	if got.Exit || got.Err != nil || got.Output != "" {
		t.Fatalf("multiline await initializer result=%+v", got)
	}
	got = s.Eval(context.Background(), `awaited`)
	if got.Exit || got.Err != nil || got.Output != "42" {
		t.Fatalf("multiline await binding result=%+v", got)
	}

	got = s.Eval(context.Background(), `let assigned = 0`)
	if got.Exit || got.Err != nil {
		t.Fatalf("assignment setup result=%+v", got)
	}
	got = s.Eval(context.Background(), "assigned = await Promise.resolve(\n 43\n)")
	if got.Exit || got.Err != nil || got.Output != "43" {
		t.Fatalf("multiline await assignment result=%+v", got)
	}
}
