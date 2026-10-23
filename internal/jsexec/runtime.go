package jsexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	lower "forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

// Result contains execution data and diagnostics detached from the VM.
type Result struct {
	Code        int              `json:"code"`
	Data        json.RawMessage  `json:"data,omitempty"`
	Diagnostics []DiagnosticInfo `json:"diagnostics,omitempty"`
	Phase       string           `json:"phase,omitempty"`
}

// Runtime owns one VM, one Loop and one entry execution. NewRuntime, Run and
// Close run on its owner goroutine; Cancel can be called from another goroutine.
type Runtime struct {
	rt               *quickjs.Runtime
	loop             *lower.Loop
	bindings         *Bindings
	ctx              context.Context
	cancel           context.CancelFunc
	observe          quickjs.Value
	observeRejection quickjs.Value
	stringify        quickjs.Value
	thrower          quickjs.Value
	stopLoop         func() bool
	ran, closed      bool
	closeErr         error
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
	// Capture Promise and JSON operations before user initialization can replace globals.
	helpers, err := rt.EvalFile("<stdlib-execution-helpers>", `(() => {
 const P=Promise,resolve=P.resolve,then=P.prototype.then,apply=Reflect.apply;
 const stringify=JSON.stringify;
 return {
   observe(value,fulfilled,rejected){const p=apply(resolve,P,[value]);apply(then,p,[fulfilled,rejected]);return p;},
   observeRejection(promise,rejected){apply(then,promise,[undefined,rejected]);},
   stringify(value){return stringify(value);},
   thrower(value){throw value;}
 };
})()`)
	if err == nil {
		r.observe, err = helpers.Get("observe")
		if err == nil {
			r.observeRejection, err = helpers.Get("observeRejection")
		}
		if err == nil {
			r.stringify, err = helpers.Get("stringify")
		}
		if err == nil {
			r.thrower, err = helpers.Get("thrower")
		}
		if err == nil {
			r.bindings, err = attach(rt, r.loop, profile, false, nil, ctx)
		}
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
	phase := "prepare"
	resultOverride := ""
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
			result.Phase = phase
			result.Diagnostics = diagnosticInfos(err)
			for i := range result.Diagnostics {
				if result.Diagnostics[i].Phase == "" {
					result.Diagnostics[i].Phase = phase
				}
				if resultOverride != "" && !isContextFailure(err) {
					result.Diagnostics[i].Code = resultOverride
					result.Diagnostics[i].Phase = phase
				}
				if result.Phase == phase && result.Diagnostics[i].Phase != "" {
					result.Phase = result.Diagnostics[i].Phase
				}
			}
		} else {
			result.Phase = "complete"
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
	var module *quickjs.Module
	if entry.Module {
		phase = "prepare"
		module, err = r.rt.PrepareModuleContext(b.ctx, entry.Name, entry.Code)
		if err != nil {
			return result, err
		}
		phase = "evaluate"
		value, err = module.EvaluateContext(b.ctx)
	} else {
		phase = "evaluate"
		value, err = r.rt.EvalFileContext(b.ctx, entry.Name, entry.Code)
	}
	if err != nil {
		return result, err
	}
	var defaultPromiseObserved, defaultPromiseRejected bool
	var defaultPromiseReason quickjs.Value
	var defaultPromiseObserverErr error
	observeDefaultPromise := func(completed bool) {
		if module == nil || defaultPromiseObserved || defaultPromiseObserverErr != nil {
			return
		}
		names, err := module.ExportNames()
		if err != nil {
			if completed {
				defaultPromiseObserverErr = err
			}
			return
		}
		hasDefault := false
		for _, name := range names {
			if name == "default" {
				hasDefault = true
				break
			}
		}
		if !hasDefault {
			return
		}
		namespace, err := module.Namespace()
		if err != nil {
			if completed {
				defaultPromiseObserverErr = err
			}
			return
		}
		defaultValue, err := namespace.Get("default")
		if err != nil {
			// A top-level-await module may not have initialized its default
			// binding yet. The completion reaction retries after it has settled.
			if completed {
				defaultPromiseObserverErr = err
			}
			return
		}
		if !defaultValue.IsPromise() {
			return
		}
		defaultPromiseObserved = true
		_, err = r.observeRejection.CallContext(b.ctx, defaultValue, func(reason quickjs.Value) {
			defaultPromiseRejected = true
			defaultPromiseReason = reason
		})
		if err != nil {
			defaultPromiseObserverErr = err
		}
	}
	if module != nil {
		// A synchronous module may have created an already rejected default
		// Promise before its completion reaction can run. Observe that exact
		// export now; the reaction below retries for top-level-await modules.
		observeDefaultPromise(false)
	}
	settled := false
	rejected := false
	observed, err := r.observe.CallContext(b.ctx, value, func() {
		settled = true
		observeDefaultPromise(true)
	}, func() {
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
	if module != nil {
		if defaultPromiseObserverErr != nil {
			phase = "serialize"
			return result, defaultPromiseObserverErr
		}
		if defaultPromiseRejected {
			phase = "serialize"
			resultOverride = codeJSResult
			_, err := r.thrower.CallContext(b.ctx, defaultPromiseReason)
			if err != nil {
				return result, fmt.Errorf("stdlib: default export Promise rejected; use `export default await ...`: %w", err)
			}
			return result, errors.New("stdlib: default export Promise rejected; use `export default await ...`")
		}
		if err := r.captureModuleData(module, &result, &phase, &resultOverride); err != nil {
			return result, err
		}
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
		retained := retainException(err)
		return Result{Code: 1, Phase: "prepare", Diagnostics: fillDiagnosticPhase(diagnosticInfos(retained), "prepare")}, retained
	}
	result, err = runtime.Run(source)
	closeErr := runtime.Close()
	if closeErr != nil {
		cleanupDiagnostics := fillDiagnosticPhase(diagnosticInfos(closeErr), "cleanup")
		if err == nil {
			result.Phase = "cleanup"
			result.Diagnostics = cleanupDiagnostics
		} else {
			result.Diagnostics = append(result.Diagnostics, cleanupDiagnostics...)
		}
	}
	err = errors.Join(err, closeErr)
	if err != nil {
		result.Code = 1
	}
	return result, err
}

func isContextFailure(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (r *Runtime) captureModuleData(module *quickjs.Module, result *Result, phase *string, override *string) error {
	ctx := r.bindings.ctx
	if err := ctx.Err(); err != nil {
		return err
	}
	names, err := module.ExportNames()
	if err != nil {
		return err
	}
	hasDefault := false
	for _, name := range names {
		if name == "default" {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		return nil
	}
	namespace, err := module.Namespace()
	if err != nil {
		return err
	}
	value, err := namespace.Get("default")
	if err != nil {
		return err
	}
	*phase = "serialize"
	if value.IsPromise() {
		*override = codeJSResult
		return fmt.Errorf("stdlib: default export is a Promise; use `export default await ...` to return its value")
	}
	encoded, err := r.stringify.CallContext(ctx, value)
	if err != nil {
		*override = codeJSResult
		return err
	}
	if !encoded.IsString() {
		*override = codeJSResult
		return fmt.Errorf("stdlib: default export is not JSON serializable; JSON.stringify returned undefined")
	}
	result.Data = json.RawMessage(encoded.String())
	return nil
}
