package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionAsyncBindingsAndRecovery(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(`[1,2,3]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mod.js"), []byte(`export const value = await Promise.resolve(42)`), 0600); err != nil {
		t.Fatal(err)
	}
	dataPath := jsString(t, filepath.Join(dir, "data.json"))
	badPath := jsString(t, filepath.Join(dir, "bad.txt"))
	duplicatePath := jsString(t, filepath.Join(dir, "duplicate.txt"))
	inspectedPath := jsString(t, filepath.Join(dir, "inspected.txt"))
	s, err := NewSession(Options{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	setup := s.Eval(ctx, `const fsPromises = await import("node:fs/promises")`)
	if setup.Exit || setup.Err != nil {
		t.Fatalf("stdlib import setup = %+v", setup)
	}
	for _, tc := range []struct{ input, out, diagnostic string }{
		{`const text = await (await import("node:fs/promises")).readFile(` + dataPath + `, "utf8")`, "", ""},
		{`JSON.parse(text).length`, "3", ""},
		{`const mod = await import("./mod.js")`, "", ""},
		{`mod.value`, "42", ""},
		{`let value = await Promise.resolve(2); // trailing, comment`, "", ""},
		{`value = await Promise.resolve(4)`, "4", ""},
		{`value`, "4", ""},
		{`var mutable = await Promise.resolve(3)`, "", ""},
		{`mutable += 1`, "4", ""},
		{`text = "changed"`, "", "constant"},
		{`const failed = await Promise.reject(new Error("await failed"))`, "", "await failed"},
		{`const failed = 8`, "", ""},
		{`failed`, "8", ""},
		{`await new Promise(() => {})`, "", "nothing is left to settle"},
		{`value`, "4", ""},
		{`const comma = await Promise.resolve("a,b")`, "", ""},
		{`comma`, "'a,b'", ""},
		{"const template = await Promise.resolve(`a${'b,c'}`)", "", ""},
		{`template`, "'ab,c'", ""},
		{`const regex = await Promise.resolve(/a,b/.source)`, "", ""},
		{`regex`, "'a,b'", ""},
		{`const comment = await Promise.resolve(7) //, ignored`, "", ""},
		{`comment`, "7", ""},
		{`const division = await Promise.resolve(8) / 2`, "", ""},
		{`division`, "4", ""},
		{`const awaitWord = "await"`, "", ""},
		{`awaitWord`, "'await'", ""},
	} {
		got := s.Eval(ctx, tc.input)
		if got.Exit || got.Output != tc.out {
			t.Fatalf("input=%q result=%+v", tc.input, got)
		}
		if tc.diagnostic == "" {
			if got.Err != nil {
				t.Fatalf("input=%s err=%v", tc.input, got.Err)
			}
		} else if got.Err == nil || !strings.Contains(Diagnostic(got.Err), tc.diagnostic) {
			t.Fatalf("input=%s err=%v", tc.input, got.Err)
		}
	}
	for _, source := range []string{
		`const a = await fsPromises.writeFile(` + badPath + `,"bad"), b = 2`,
		`await fsPromises.writeFile(` + badPath + `,"bad"); console.log("bad")`,
		`const {x} = await fsPromises.writeFile(` + badPath + `,"bad")`,
		`return = await fsPromises.writeFile(` + badPath + `,"bad")`,
	} {
		got := s.Eval(ctx, source)
		if got.Exit || got.Err == nil {
			t.Fatalf("source=%q got=%+v", source, got)
		}
		if _, err := os.Stat(filepath.Join(dir, "bad.txt")); !os.IsNotExist(err) {
			t.Fatalf("unsupported input executed I/O: %v", err)
		}
	}
	got := s.Eval(ctx, `const text = await fsPromises.writeFile(`+duplicatePath+`,"ran")`)
	data, readErr := os.ReadFile(filepath.Join(dir, "duplicate.txt"))
	if got.Err == nil || got.Exit || readErr != nil || string(data) != "ran" {
		t.Fatalf("duplicate result=%+v data=%q err=%v", got, data, readErr)
	}
	got = s.Eval(ctx, `({get value(){fsPromises.writeFile(`+inspectedPath+`,"done"); return 1;}})`)
	data, readErr = os.ReadFile(filepath.Join(dir, "inspected.txt"))
	if got.Err != nil || got.Exit || readErr != nil || string(data) != "done" {
		t.Fatalf("formatter I/O result=%+v data=%q err=%v", got, data, readErr)
	}
}

func TestSessionFormattingCancellation(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seed := s.Eval(context.Background(), `42`)
	if seed.Err != nil || seed.Exit || seed.Output != "42" {
		t.Fatalf("seed=%+v", seed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := s.Eval(ctx, `await Promise.resolve({get value(){while(true){}}})`)
	if result.Exit || !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("result=%+v", result)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result = s.Eval(ctx, `_`)
	if result.Exit || result.Err != nil || result.Output != "42" {
		t.Fatalf("cancelled input changed session state: %+v", result)
	}
	result = s.Eval(ctx, `1 + 1`)
	if result.Exit || result.Err != nil || result.Output != "2" {
		t.Fatalf("session did not recover after formatter cancellation: %+v", result)
	}
}

func TestSessionCancellationDropsQueuedInfiniteReaction(t *testing.T) {
	type results struct {
		first     Evaluation
		second    Evaluation
		ctxErr    error
		busy      bool
		unhandled int
		err       error
	}
	done := make(chan results, 1)
	go func() {
		s, err := NewSession(Options{})
		if err != nil {
			done <- results{err: err}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		first := s.Eval(ctx, `await Promise.resolve({get value(){Promise.resolve().then(()=>{while(true){}}); return 1;}})`)
		cancel()
		busy := s.h.rt.Busy()
		unhandled := len(s.h.unhandled)
		second := s.Eval(context.Background(), `1 + 1`)
		ctxErr := s.h.rt.Context().Err()
		s.Close()
		done <- results{first: first, second: second, ctxErr: ctxErr, busy: busy, unhandled: unhandled}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.first.Exit || !errors.Is(got.first.Err, context.DeadlineExceeded) {
			t.Fatalf("cancelled evaluation result=%+v", got.first)
		}
		if got.second.Exit || got.second.Err != nil || got.second.Output != "2" {
			t.Fatalf("session did not recover after dropping queued reaction: first=%+v second=%+v runtimeCtxErr=%v busy=%v unhandled=%d", got.first, got.second, got.ctxErr, got.busy, got.unhandled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled queued reaction kept running during cleanup")
	}
}
