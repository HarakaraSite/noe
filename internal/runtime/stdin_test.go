package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadStdinTextAndSharedPromise(t *testing.T) {
	for _, input := range []string{"", "[100,200,300]\n", "雪\n日本語\n", "last line"} {
		t.Run(input, func(t *testing.T) {
			reader := &borrowedStdin{Reader: strings.NewReader(input)}
			var output bytes.Buffer
			source := `const first = noe.readStdin();
const second = noe.readStdin();
if (first !== second) throw new Error("competing stdin promises");
const values = await Promise.all([first, second]);
values.push(await noe.readStdin());
console.log(JSON.stringify(values));`
			code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "stdin.js"), source}, Options{Stdin: reader, Stdout: &output})
			if code != 0 || err != nil {
				t.Fatalf("code=%d err=%v out=%q", code, err, &output)
			}
			var values []string
			if err := json.Unmarshal(output.Bytes(), &values); err != nil || len(values) != 3 {
				t.Fatalf("output=%q decode=%v", &output, err)
			}
			for _, value := range values {
				if value != input {
					t.Fatalf("stdin=%q want=%q", value, input)
				}
			}
			if reader.closed.Load() {
				t.Fatal("runtime closed borrowed stdin")
			}
		})
	}
	var output bytes.Buffer
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "default.js"), `console.log(JSON.stringify(await noe.readStdin()));`}, Options{Stdout: &output})
	if code != 0 || err != nil || output.String() != "\"\"\n" {
		t.Fatalf("nil stdin: code=%d err=%v output=%q", code, err, &output)
	}
}

func TestReadStdinErrors(t *testing.T) {
	for _, source := range []string{`await noe.readStdin();`, `noe.readStdin();`} {
		code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "error.js"), source}, Options{Stdin: brokenStdin{}})
		if code != 1 || err == nil || !strings.Contains(err.Error(), "stdin read failed") {
			t.Fatalf("source=%q code=%d err=%v", source, code, err)
		}
	}
	var output bytes.Buffer
	source := `const p = noe.readStdin();
try { await p; } catch (e) { console.log(String(e).includes("stdin read failed")); }
try { await noe.readStdin(); } catch (e) { console.log(p === noe.readStdin()); }`
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "caught.js"), source}, Options{Stdin: brokenStdin{}, Stdout: &output})
	if code != 0 || err != nil || output.String() != "true\ntrue\n" {
		t.Fatalf("caught failure: code=%d err=%v output=%q", code, err, &output)
	}
}

func TestSessionDoesNotExposeReadStdin(t *testing.T) {
	reader := &borrowedStdin{Reader: brokenStdin{}}
	s, err := NewSession(Options{Stdin: reader})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result := s.Eval(context.Background(), `typeof noe.readStdin`)
	if result.Exit || result.Err != nil || result.Output != "'undefined'" || reader.reads.Load() != 0 {
		t.Fatalf("evaluation=%+v stdin reads=%d", result, reader.reads.Load())
	}
}

func TestReadStdinNormalEndWaitsForEOF(t *testing.T) {
	reader := newWaitingStdin()
	defer reader.unblock()
	var output bytes.Buffer
	done := make(chan stdinExecution, 1)
	name := filepath.Join(t.TempDir(), "drain.js")
	go func() {
		code, err := Execute(context.Background(), Source{name, `noe.readStdin().then(text => console.log(JSON.stringify(text)));`}, Options{Stdin: reader, Stdout: &output})
		done <- stdinExecution{code, err}
	}()
	waitStdinStarted(t, reader)
	select {
	case result := <-done:
		t.Fatalf("runtime returned before stdin EOF: %+v", result)
	case <-time.After(30 * time.Millisecond):
	}
	reader.unblock()
	result := waitStdinExecution(t, done)
	if result.code != 0 || result.err != nil || output.String() != "\"\"\n" {
		t.Fatalf("result=%+v output=%q", result, &output)
	}
}

func TestReadStdinCancellationDoesNotWaitOrCloseReader(t *testing.T) {
	reader := newWaitingStdin()
	defer reader.unblock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan stdinExecution, 1)
	var output bytes.Buffer
	name := filepath.Join(t.TempDir(), "cancel.js")
	go func() {
		code, err := Execute(ctx, Source{name, `await noe.readStdin(); console.log("late");`}, Options{Stdin: reader, Stdout: &output})
		done <- stdinExecution{code, err}
	}()
	waitStdinStarted(t, reader)
	cancel()
	result := waitStdinExecution(t, done)
	if result.code != 1 || !errors.Is(result.err, context.Canceled) || reader.closed.Load() || output.Len() != 0 {
		t.Fatalf("result=%+v closed=%v output=%q", result, reader.closed.Load(), &output)
	}
	reader.unblock()
}

func TestReadStdinExplicitExitWhileBlocked(t *testing.T) {
	reader := newWaitingStdin()
	defer reader.unblock()
	var output bytes.Buffer
	h, err := newHost(Options{Stdin: reader, Stdout: &output})
	if err != nil {
		t.Fatal(err)
	}
	defer h.rt.Close()
	if err := h.rt.Set("waitForRead", func() error {
		select {
		case <-reader.started:
			return nil
		case <-time.After(3 * time.Second):
			return errors.New("stdin read did not start")
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	code, err := h.run(ctx, Source{filepath.Join(t.TempDir(), "exit.js"), `noe.readStdin().then(() => console.log("late")); waitForRead(); noe.exit(7);`})
	if code != 7 || err != nil || reader.closed.Load() || output.Len() != 0 {
		t.Fatalf("code=%d err=%v closed=%v output=%q", code, err, reader.closed.Load(), &output)
	}
	reader.unblock()
}

type stdinExecution struct {
	code int
	err  error
}

type borrowedStdin struct {
	io.Reader
	closed atomic.Bool
	reads  atomic.Int32
}

func (r *borrowedStdin) Read(p []byte) (int, error) {
	r.reads.Add(1)
	return r.Reader.Read(p)
}

func (r *borrowedStdin) Close() error { r.closed.Store(true); return nil }

type brokenStdin struct{}

func (brokenStdin) Read([]byte) (int, error) { return 0, errors.New("stdin read failed") }

type waitingStdin struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	closed  atomic.Bool
}

func newWaitingStdin() *waitingStdin {
	return &waitingStdin{started: make(chan struct{}), release: make(chan struct{})}
}

func (r *waitingStdin) Read([]byte) (int, error) {
	close(r.started)
	<-r.release
	return 0, io.EOF
}

func (r *waitingStdin) Close() error { r.closed.Store(true); r.unblock(); return nil }
func (r *waitingStdin) unblock()     { r.once.Do(func() { close(r.release) }) }

func waitStdinStarted(t *testing.T, r *waitingStdin) {
	t.Helper()
	select {
	case <-r.started:
	case <-time.After(3 * time.Second):
		t.Fatal("stdin reader did not start")
	}
}

func waitStdinExecution(t *testing.T, done <-chan stdinExecution) stdinExecution {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("runtime waited for blocked stdin")
		return stdinExecution{}
	}
}
