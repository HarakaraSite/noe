package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestSessionAsyncBindingsAndRecovery(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(`[1,2,3]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mod.js"), []byte(`export const value = await Promise.resolve(42)`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(Options{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, tc := range []struct{ input, out, diagnostic string }{
		{`const text = await noe.readTextFile("data.json")`, "", ""},
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
		`const a = await noe.writeTextFile("bad.txt","bad"), b = 2`,
		`await noe.writeTextFile("bad.txt","bad"); console.log("bad")`,
		`const {x} = await noe.writeTextFile("bad.txt","bad")`,
		`return = await noe.writeTextFile("bad.txt","bad")`,
	} {
		got := s.Eval(ctx, source)
		if got.Exit || got.Err == nil {
			t.Fatalf("source=%q got=%+v", source, got)
		}
		if _, err := os.Stat(filepath.Join(dir, "bad.txt")); !os.IsNotExist(err) {
			t.Fatalf("unsupported input executed I/O: %v", err)
		}
	}
	got := s.Eval(ctx, `const text = await noe.writeTextFile("duplicate.txt","ran")`)
	data, readErr := os.ReadFile(filepath.Join(dir, "duplicate.txt"))
	if got.Err == nil || got.Exit || readErr != nil || string(data) != "ran" {
		t.Fatalf("duplicate result=%+v data=%q err=%v", got, data, readErr)
	}
	got = s.Eval(ctx, `({get value(){noe.writeTextFile("inspected.txt","done"); return 1;}})`)
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

func TestSessionCancellationUnrefsHostWorkAndSuppressesLateCompletion(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})
	resumeOwner := make(chan struct{})
	type stage struct {
		first  Evaluation
		second Evaluation
		busy   bool
		err    error
	}
	stageResult := make(chan stage, 1)
	finalResult := make(chan Evaluation, 1)
	var stdout bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		s, err := NewSession(Options{Stdout: &stdout})
		if err != nil {
			stageResult <- stage{err: err}
			return
		}
		if err := s.h.rt.Set("blockWork", func() quickjs.Value {
			return s.h.startAsyncWork(func(context.Context) (any, error) {
				close(started)
				<-release
				close(returned)
				return "late", nil
			})
		}); err != nil {
			stageResult <- stage{err: err}
			return
		}
		first := s.Eval(ctx, `await blockWork().then(value => console.log("stale", value))`)
		second := s.Eval(context.Background(), `1 + 1`)
		stageResult <- stage{first: first, second: second, busy: s.h.rt.Busy()}
		<-resumeOwner
		final := s.Eval(context.Background(), `2 + 2`)
		s.Close()
		finalResult <- final
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("host worker did not start")
	}
	cancel()
	var got stage
	select {
	case got = <-stageResult:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled input waited for a context-ignoring host worker")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.first.Exit || !errors.Is(got.first.Err, context.Canceled) {
		t.Fatalf("cancelled evaluation result=%+v", got.first)
	}
	if got.second.Exit || got.second.Err != nil || got.second.Output != "2" || got.busy {
		t.Fatalf("host work still kept the runtime busy after cancellation: %+v", got)
	}

	close(release)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("late host worker did not return after release")
	}
	time.Sleep(20 * time.Millisecond)
	close(resumeOwner)
	select {
	case final := <-finalResult:
		if final.Exit || final.Err != nil || final.Output != "4" {
			t.Fatalf("session after late worker completion=%+v", final)
		}
	case <-time.After(time.Second):
		t.Fatal("session owner did not resume")
	}
	if stdout.String() != "" {
		t.Fatalf("late completion ran a stale Promise reaction: %q", stdout.String())
	}
}

func TestSessionFIFOReadCancellationDoesNotLeakLateOpen(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "input.fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	s, err := NewSession(Options{Cwd: dir, Stdout: &stdout})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	result := s.Eval(ctx, `await noe.readTextFile("input.fifo").then(value => { console.log("stale FIFO", value); return value; })`)
	cancel()
	if result.Exit || !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("FIFO read cancellation result=%+v", result)
	}
	next := s.Eval(context.Background(), `40 + 2`)
	if next.Exit || next.Err != nil || next.Output != "42" {
		t.Fatalf("session did not continue while FIFO open was pending: %+v", next)
	}

	writerDone := make(chan error, 1)
	go func() {
		writer, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err == nil {
			_, err = writer.Write([]byte("late data"))
			if closeErr := writer.Close(); err == nil {
				err = closeErr
			}
		}
		writerDone <- err
	}()
	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("late FIFO writer did not release the canceled open")
	}
	time.Sleep(20 * time.Millisecond)
	if stdout.String() != "" {
		t.Fatalf("late FIFO open emitted a stale Promise reaction: %q", stdout.String())
	}
	last := s.Eval(context.Background(), `_`)
	if last.Exit || last.Err != nil || last.Output != "42" {
		t.Fatalf("late FIFO work contaminated the next input: %+v", last)
	}
}

func TestSessionInputCancellationCancelsHostWorkAndRecovers(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
			close(cancelled)
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()
	url, err := json.Marshal(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	s, err := NewSession(Options{Stdout: &stdout})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	setup := s.Eval(ctx, `let retained = 40`)
	if setup.Exit || setup.Err != nil {
		t.Fatalf("setup=%+v", setup)
	}

	inputCtx, cancelInput := context.WithCancel(ctx)
	cancelDone := make(chan struct{})
	go func() {
		defer close(cancelDone)
		select {
		case <-started:
		case <-ctx.Done():
		}
		cancelInput()
	}()
	input := `await (retained = 41,
	  fetch(` + string(url) + `).then(() => { console.log("stale host output"); throw new Error("stale host rejection"); }),
	  Promise.resolve().then(() => {
	    Promise.resolve().then(() => {
	      const until = Date.now() + 100;
	      while (Date.now() < until) {}
	      console.log("stale queued output");
	      throw new Error("stale queued rejection");
	    });
	    while (true) {}
	  })
	)`
	result := s.Eval(inputCtx, input)
	<-cancelDone
	if result.Exit || !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("cancelled input result=%+v", result)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("input cancellation did not cancel the HTTP request")
	}
	if stdout.String() != "" {
		t.Fatalf("cancelled input emitted a stale host-work reaction: %q", stdout.String())
	}

	result = s.Eval(ctx, `retained`)
	if result.Exit || result.Err != nil || result.Output != "41" {
		t.Fatalf("cancelled input lost prior mutation or session recovery failed: %+v", result)
	}
	result = s.Eval(ctx, `console.log("fresh output")`)
	if result.Exit || result.Err != nil || stdout.String() != "fresh output\n" {
		t.Fatalf("later input was contaminated: result=%+v stdout=%q", result, stdout.String())
	}
}
