package jsexec

import (
	"context"
	"encoding/json"
	"errors"

	lower "forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// Result contains detached execution data. JS values stay with their VM owner.
type Result struct{ Code int }

// Runtime owns one VM, one Loop and one entry execution. NewRuntime, Run and
// Close run on its owner goroutine; Cancel can be called from another goroutine.
type Runtime struct {
	rt          *quickjs.Runtime
	loop        *lower.Loop
	bindings    *Bindings
	ctx         context.Context
	cancel      context.CancelFunc
	observe     quickjs.Value
	stopLoop    func() bool
	ran, closed bool
	closeErr    error
}

func NewRuntime(ctx context.Context, profile Profile) (*Runtime, error) {
	if ctx == nil {
		return nil, errors.New("stdlib: execution context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	rt := quickjs.New()
	r := &Runtime{rt: rt, ctx: ctx, cancel: cancel, loop: lower.NewLoop(rt)}
	rt.OnClose(r.loop.Close)
	// Capture Promise operations before user initialization can replace globals.
	observe, err := rt.EvalFile("<stdlib-await>", `(() => {
 const P=Promise,resolve=P.resolve,then=P.prototype.then,apply=Reflect.apply;
 return (value,fulfilled,rejected) => {
   const p=apply(resolve,P,[value]); apply(then,p,[fulfilled,rejected]); return p;
 };
})()`)
	if err == nil {
		r.observe = observe
		r.bindings, err = attach(rt, r.loop, profile, false, nil, ctx)
	}
	if err != nil {
		err = retainException(err)
		cancel()
		rt.Close()
		return nil, err
	}
	r.stopLoop = context.AfterFunc(r.bindings.ctx, r.loop.Close)
	return r, nil
}

func (r *Runtime) Bindings() *Bindings { return r.bindings }
func (r *Runtime) Cancel() {
	if r != nil && r.cancel != nil {
		r.cancel()
	}
}

func (r *Runtime) Run(source Source) (result Result, err error) {
	result.Code = 1
	if r == nil || r.closed {
		return result, errors.New("stdlib: runtime is closed")
	}
	if r.ran {
		return result, errors.New("stdlib: runtime already ran an entry")
	}
	r.ran = true
	b := r.bindings
	defer func() {
		// Caller cancellation wins; diagnostic causes are detached before teardown.
		failure := b.failure()
		if callerErr := r.ctx.Err(); callerErr != nil {
			err = errors.Join(callerErr, failure)
		} else if failure != nil {
			err = failure
		} else if b.exitRequested {
			result.Code, err = b.exitCode, nil
		}
		err = b.retainException(err)
		if callerErr := r.ctx.Err(); callerErr != nil {
			err = errors.Join(callerErr, b.failure())
		}
		if err != nil {
			result.Code = 1
		}
	}()
	if err := r.ctx.Err(); err != nil {
		return result, err
	}
	entry, err := b.prepare(source, source.Module)
	if err != nil {
		return result, err
	}
	driveCtx, stopDrive := context.WithCancel(b.ctx)
	defer stopDrive()
	var value quickjs.Value
	if entry.Module {
		name, _ := json.Marshal(entry.Name)
		value, err = r.rt.EvalFileContext(b.ctx, "<stdlib-entry>", `(() => {const p=import(`+string(name)+`);p.catch(()=>{});return p;})()`)
	} else {
		value, err = r.rt.EvalFileContext(b.ctx, entry.Name, entry.Code)
	}
	if err != nil {
		return result, err
	}
	settled := false
	rejected := false
	observed, err := r.observe.CallContext(b.ctx, value, func() { settled = true }, func() {
		settled = true
		rejected = true
		stopDrive()
	})
	if err != nil {
		return result, err
	}
	driveErr := r.loop.Run(driveCtx)
	if rejected {
		// Await only after the owner leaves the job runner. The engine retains
		// the native Go cause on the rejected Promise; a string/value snapshot
		// here would lose errors.Is/errors.As before detached diagnostics run.
		_, err := observed.Await(b.ctx)
		return result, err
	}
	if err := b.ctx.Err(); err != nil {
		return result, err
	}
	if driveErr != nil {
		return result, driveErr
	}
	if !settled {
		return result, quickjs.ErrNeverSettles
	}
	result.Code = 0
	if code, ok := lower.ExitCode(r.rt); ok {
		result.Code = code
	}
	return result, nil
}

// Close cancels and joins owned work, runs owner cleanup and releases the VM.
// Supplied readers, writers, loader factories and binding services are borrowed.
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	if r.closed {
		return r.closeErr
	}
	r.closed = true
	r.cancel()
	if r.stopLoop != nil {
		r.stopLoop()
	}
	if r.bindings != nil {
		r.closeErr = r.bindings.Close()
	}
	r.loop.Close()
	r.rt.Close()
	if r.bindings != nil && r.bindings.io != nil {
		r.closeErr = errors.Join(r.closeErr, r.bindings.io.Err())
	}
	return r.closeErr
}

func Execute(ctx context.Context, source Source, profile Profile) (result Result, err error) {
	runtime, err := NewRuntime(ctx, profile)
	if err != nil {
		return Result{Code: 1}, err
	}
	result, err = runtime.Run(source)
	closeErr := runtime.Close()
	err = errors.Join(err, closeErr)
	if err != nil {
		result.Code = 1
	}
	return result, err
}
