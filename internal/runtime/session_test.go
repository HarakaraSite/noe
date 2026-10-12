package runtime

import (
	"bytes"
	"context"
	"strings"
	"testing"
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
	result = s.Eval(context.Background(), `({ get value() { noe.exit(7); } })`)
	if !result.Exit || result.Code != 7 || result.Err != nil {
		t.Fatalf("result=%+v", result)
	}
}
