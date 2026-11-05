package stdlib

import (
	"context"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestLoopTimerWakeAndRearm(t *testing.T) {
	for _, until := range []bool{false, true} {
		name := "Run"
		if until {
			name = "RunUntil"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rt := quickjs.New()
				defer rt.Close()
				loop := NewLoop(rt)
				defer loop.Close()
				if err := Timers(rt, loop); err != nil {
					t.Fatal(err)
				}
				eval := func(source string) {
					t.Helper()
					if _, err := rt.Eval(source); err != nil {
						t.Fatal(err)
					}
				}
				start := time.Now()
				var names []string
				var times []time.Duration
				done := make(chan struct{})
				if err := rt.Set("record", func(name string) {
					names = append(names, name)
					times = append(times, time.Since(start))
					if name == "late" {
						close(done)
					}
				}); err != nil {
					t.Fatal(err)
				}
				eval(`const obsolete = setTimeout(() => record("obsolete"), 1000)`)
				loop.Begin()
				go func() {
					time.Sleep(10 * time.Millisecond)
					loop.Post(func() {
						// Wake an existing wait and move its deadline earlier.
						eval(`clearTimeout(obsolete); setTimeout(() => record("early"), 20)`)
					})
					time.Sleep(40 * time.Millisecond)
					loop.Post(func() {
						// The early timer has fired and the loop has been waiting
						// without a timer. Arm another, later deadline.
						eval(`setTimeout(() => record("late"), 100)`)
						loop.Done()
					})
				}()
				var err error
				if until {
					err = loop.RunUntil(context.Background(), done)
				} else {
					err = loop.Run(context.Background())
				}
				if err != nil {
					t.Fatal(err)
				}
				// A later run must still deliver its own timer normally.
				eval(`setTimeout(() => record("resume"), 10)`)
				if err := loop.Run(context.Background()); err != nil {
					t.Fatal(err)
				}
				wantNames := []string{"early", "late", "resume"}
				wantTimes := []time.Duration{30 * time.Millisecond, 150 * time.Millisecond, 160 * time.Millisecond}
				if len(names) != len(wantNames) {
					t.Fatalf("callbacks = %v at %v", names, times)
				}
				for i := range wantNames {
					if names[i] != wantNames[i] || times[i] != wantTimes[i] {
						t.Fatalf("callbacks = %v at %v; want %v at %v", names, times, wantNames, wantTimes)
					}
				}
			})
		})
	}
}

// BenchmarkLoopTimerWake measures host work repeatedly waking a loop while
// a distant JavaScript timer is armed. Sample retained memory before Run
// returns, when per-wait defers would still hold every old Go timer alive.
func BenchmarkLoopTimerWake(b *testing.B) {
	for _, until := range []bool{false, true} {
		name := "Run"
		if until {
			name = "RunUntil"
		}
		b.Run(name, func(b *testing.B) {
			rt := quickjs.New()
			defer rt.Close()
			loop := NewLoop(rt)
			defer loop.Close()
			if err := Timers(rt, loop); err != nil {
				b.Fatal(err)
			}
			if _, err := rt.Eval(`const distant = setTimeout(() => {}, 3600000)`); err != nil {
				b.Fatal(err)
			}
			loop.Begin()
			ready, start := make(chan struct{}), make(chan struct{})
			ack, done := make(chan struct{}), make(chan struct{})
			sampled, producerDone := make(chan struct{}), make(chan struct{})
			var retained uint64
			acknowledge := func() { ack <- struct{}{} }
			go func() {
				defer close(producerDone)
				close(ready)
				<-start
				for range b.N {
					loop.Post(acknowledge)
					<-ack
				}
				loop.Post(func() {
					b.StopTimer()
					runtime.GC()
					var memory runtime.MemStats
					runtime.ReadMemStats(&memory)
					retained = memory.HeapAlloc
					close(sampled)
					if _, err := rt.Eval(`clearTimeout(distant)`); err != nil {
						b.Error(err)
					}
					loop.Done()
					close(done)
				})
				<-sampled
			}()
			<-ready
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)
			b.ReportAllocs()
			b.ResetTimer()
			close(start)
			var err error
			if until {
				err = loop.RunUntil(context.Background(), done)
			} else {
				err = loop.Run(context.Background())
			}
			<-producerDone
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(int64(retained)-int64(before.HeapAlloc))/float64(b.N), "heap-delta-B/post")
		})
	}
}
