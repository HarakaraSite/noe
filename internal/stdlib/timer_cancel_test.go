package stdlib

import (
	"context"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// An earlier live timer must not keep cancelled callbacks and their arguments
// reachable through the timer queue for the rest of a long-running execution.
func TestTimerCancellationReleasesQueue(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	defer loop.Close()
	if err := Timers(rt, loop); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`
const live = setInterval(() => {}, 1000);
for (let i = 0; i < 1024; i++) {
  const id = setTimeout(value => value, 3600000, new Uint8Array(4096));
  clearTimeout(id);
  clearTimeout(id);
}
`); err != nil {
		t.Fatal(err)
	}
	if len(loop.timers.entries) != 1 || len(loop.timers.byID) != 1 {
		t.Fatalf("cancelled timers remain reachable: queue=%d live=%d", len(loop.timers.entries), len(loop.timers.byID))
	}
	if _, err := rt.Eval(`clearInterval(live)`); err != nil {
		t.Fatal(err)
	}
	if len(loop.timers.entries) != 0 {
		t.Fatal("clearing the last timer retained its callback")
	}
}

func TestTimerCancellationAfterScopeDiscard(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	defer loop.Close()
	scope, err := NewExecutionScope(loop, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := int64(1); i <= 8; i++ {
		entry := &timer{id: i, at: now.Add(time.Duration(i) * time.Hour)}
		if i%2 == 1 {
			entry.scope = scope
		}
		loop.timers.add(entry)
	}
	scope.Close()
	// Removing scope-owned timers changes the surviving entries' indices even
	// when the compacted slice is already a heap and heap.Init swaps nothing.
	for _, id := range []int64{6, 2, 8, 4} {
		loop.timers.cancel(id)
	}
	if len(loop.timers.entries) != 0 || len(loop.timers.byID) != 0 {
		t.Fatal("timers survived cancellation after scope cleanup")
	}
}

func BenchmarkTimerCancellation(b *testing.B) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	defer loop.Close()
	if err := Timers(rt, loop); err != nil {
		b.Fatal(err)
	}
	batch, err := rt.Eval(`(function() {
  setInterval(() => {}, 1000);
  for (let i = 0; i < 1024; i++) {
    const id = setTimeout(value => value, 3600000 + (i * 73 % 1024), new Uint8Array(4096));
    clearTimeout(id);
  }
})`)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := batch.Call(); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		b.ReportMetric(float64(len(loop.timers.entries)), "retained-timers/batch")
		loop.timers = timerQueue{}
		b.StartTimer()
	}
}
