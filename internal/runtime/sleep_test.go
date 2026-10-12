package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSleepValidationAndZero(t *testing.T) {
	source := fmt.Sprintf(`
for (const ms of [undefined, null, true, "1", {}, [], 1n, Symbol("ms")]) {
  const pending = noe.sleep(ms);
  if (!(pending instanceof Promise)) throw new Error("sleep did not return a Promise");
  let rejected = false;
  try { await pending; } catch(e) { rejected = e instanceof TypeError; }
  if (!rejected) throw new Error("invalid type accepted");
}
for (const ms of [-1, .5, NaN, Infinity, -Infinity, %d, Number.MAX_SAFE_INTEGER]) {
  let rejected = false;
  try { await noe.sleep(ms); } catch(e) { rejected = e instanceof RangeError; }
  if (!rejected) throw new Error("invalid duration accepted");
}
for (const ms of [0, -0]) {
  if (await noe.sleep(ms) !== undefined) throw new Error("sleep return value");
}
console.log("valid");`, maxSleepMilliseconds+1)
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "sleep.js"), source}, Options{Stdout: &out})
	if code != 0 || err != nil || out.String() != "valid\n" {
		t.Fatalf("code=%d err=%v out=%q", code, err, out.String())
	}
	// The maximum supported duration is accepted, then explicit exit cancels
	// it. We do not wait for the timer or rely on a rounded duration value.
	code, err = Execute(ctx, Source{filepath.Join(t.TempDir(), "max.js"), fmt.Sprintf(`const p=noe.sleep(%d); let failure;
p.catch(error=>{failure=error}); await Promise.resolve(); await Promise.resolve();
if (failure) throw failure; noe.exit(0);`, maxSleepMilliseconds)}, Options{})
	if code != 0 || err != nil {
		t.Fatalf("maximum duration: code=%d err=%v", code, err)
	}
}

func TestSleepDelayAndUnawaitedCompletion(t *testing.T) {
	var out bytes.Buffer
	h, err := newHost(Options{Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	defer h.rt.Close()
	var started time.Time
	if err := h.rt.Set("mark", func() { started = time.Now() }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	code, err := h.run(ctx, Source{filepath.Join(t.TempDir(), "sleep.js"), `mark(); noe.sleep(30).then(value=>console.log(value === undefined)); console.log("scheduled");`})
	if code != 0 || err != nil || out.String() != "scheduled\ntrue\n" {
		t.Fatalf("code=%d err=%v out=%q", code, err, out.String())
	}
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
		t.Fatalf("millisecond delay expired early: %s", elapsed)
	}
}

func TestSleepSessionCancellationAndRecovery(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if result := s.Eval(context.Background(), `var retained = 42`); result.Err != nil {
		t.Fatal(result.Err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.h.rt.Set("cancelInput", cancel); err != nil {
		t.Fatal(err)
	}
	result := s.Eval(ctx, `noe.sleep(60000).then(()=>{globalThis.staleSleep = true}); cancelInput();`)
	if result.Exit || !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("cancelled=%+v", result)
	}
	result = s.Eval(context.Background(), `[retained + 1, typeof staleSleep]`)
	if result.Exit || result.Err != nil || result.Output != "[ 43, 'undefined' ]" {
		t.Fatalf("recovery=%+v", result)
	}
	result = s.Eval(context.Background(), `await noe.sleep(-1)`)
	if result.Exit || result.Err == nil || !strings.Contains(result.Err.Error(), "milliseconds must") {
		t.Fatalf("validation=%+v", result)
	}
	result = s.Eval(context.Background(), `await noe.sleep(0)`)
	if result.Exit || result.Err != nil || result.Output != "" {
		t.Fatalf("zero=%+v", result)
	}
}
