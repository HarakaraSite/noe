package stdlib_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type combinedCancellationInput struct {
	started chan struct{}
	stopped chan struct{}
	closed  atomic.Bool
}

func (r *combinedCancellationInput) Read([]byte) (int, error) {
	return 0, errors.New("unexpected synchronous input")
}

func (r *combinedCancellationInput) ReadContext(ctx context.Context, _ []byte) (int, error) {
	close(r.started)
	<-ctx.Done()
	close(r.stopped)
	return 0, ctx.Err()
}

func (r *combinedCancellationInput) Close() error {
	r.closed.Store(true)
	return nil
}

func TestCombinedSignalCancelsNativeStdinAndPreservesBorrowedInput(t *testing.T) {
	input := &combinedCancellationInput{started: make(chan struct{}), stopped: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "memory:combined-stdin", Virtual: true, Module: true, Code: `
import {stdin} from "node:process";
const controller=new AbortController();
const signal=AbortSignal.any([controller.signal]);
const sink=new WritableStream();
const piping=stdin.pipeTo(sink,{signal});
const check=()=>{if(inputStarted())controller.abort(0);else setTimeout(check,0);};
check();
const failure=await piping.then(()=>"unexpected success",error=>error);
if(failure!==0 || signal.reason!==0 || stdin.locked || sink.locked)throw Error("native combined cancellation");
`}, stdlib.Profile{Environment: stdlib.Environment{Stdin: input}, Modules: stdlib.Modules{Stdio: true},
		Initialize: func(_ context.Context, bindings *stdlib.Bindings) (func(), error) {
			return nil, bindings.Runtime().Set("inputStarted", func() bool {
				select {
				case <-input.started:
					return true
				default:
					return false
				}
			})
		}})
	if err != nil || result.Code != 0 || input.closed.Load() {
		t.Fatalf("combined cancellation result=%+v closed=%v error=%s", result, input.closed.Load(), stdlib.Diagnostic(err))
	}
	select {
	case <-input.stopped:
	default:
		t.Fatal("native input worker survived cancellation")
	}
}
