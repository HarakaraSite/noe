package jsexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"

	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
	lower "forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

var nextRuntimeID atomic.Uint64

// Result contains detached execution data. JS values stay with their VM owner.
type Result struct {
	Code        int                          `json:"Code"`
	Data        json.RawMessage              `json:"data,omitempty"`
	Diagnostics []executionresult.Diagnostic `json:"diagnostics,omitempty"`
}

// Runtime owns one VM, one Loop and one entry execution. NewRuntime, Run and
// Close run on its owner goroutine; Cancel can be called from another goroutine.
type Runtime struct {
	rt            *quickjs.Runtime
	loop          *lower.Loop
	bindings      *Bindings
	ctx           context.Context
	cancel        context.CancelFunc
	observe       quickjs.Value
	runDefault    quickjs.Value
	stringify     quickjs.Value
	helperName    string
	acceptDefault func(bool, quickjs.Value, quickjs.Value)
	dataValue     quickjs.Value
	hasData       bool
	defaultWait   quickjs.Value
	awaitDefault  bool
	stopLoop      func() bool
	ran, closed   bool
	closeErr      error
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
	helperName := fmt.Sprintf("noe:internal/jsexec/%d", nextRuntimeID.Add(1))
	r := &Runtime{rt: rt, ctx: ctx, cancel: cancel, loop: lower.NewLoop(rt), helperName: helperName}
	rt.OnClose(r.loop.Close)
	// Capture every operation used by result waiting and serialization before
	// profile initialization or user modules can replace the corresponding globals.
	captured, err := rt.EvalFileContext(ctx, "<stdlib-intrinsics>", `(() => {
 const P=Promise,resolve=P.resolve,then=P.prototype.then,apply=Reflect.apply;
	 const owns=Object.prototype.hasOwnProperty,jsonObject=JSON,stringify=JSON.stringify;
 const observe=(value,fulfilled,rejected) => {
   const p=apply(resolve,P,[value]); apply(then,p,[fulfilled,rejected]); return p;
 };
 const run=(namespace,accept) => {
   if(!apply(owns,namespace,["default"])) { accept(false,undefined,undefined); return; }
   const p=apply(resolve,P,[namespace.default]);
   accept(true,undefined,p);
   return apply(then,p,[value=>{accept(true,value,undefined);},error=>{throw error;}]);
 };
	 const encode=value=>apply(stringify,jsonObject,[value]);
	 return {observe,run,encode};
})()`)
	if err == nil {
		r.observe, _ = captured.Get("observe")
		r.runDefault, _ = captured.Get("run")
		r.stringify, _ = captured.Get("encode")
		r.acceptDefault = func(present bool, value, promise quickjs.Value) {
			if !present {
				r.hasData = false
				return
			}
			if !promise.IsUndefined() {
				r.hasData = true
				r.defaultWait = promise
				r.awaitDefault = true
				return
			}
			r.hasData = true
			r.dataValue = value
		}
	}
	if err == nil {
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
		err = errors.New("stdlib: runtime is closed")
		result.Diagnostics = diagnosticsFor(err, "setup")
		return result, err
	}
	if r.ran {
		err = errors.New("stdlib: runtime already ran an entry")
		result.Diagnostics = diagnosticsFor(err, "setup")
		return result, err
	}
	r.ran = true
	b := r.bindings
	stage := "setup"
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
			result.Data = nil
			err = &executionStageError{stage: stage, err: err}
			result.Diagnostics = diagnosticsFor(err, stage)
		}
	}()
	if err := r.ctx.Err(); err != nil {
		return result, err
	}
	entry, err := b.prepare(source, source.Module)
	if err != nil {
		return result, err
	}
	var wrapperName string
	if entry.Module {
		wrapperName, err = r.installEntryWrapper(b, entry.Name)
		if err != nil {
			return result, err
		}
	}
	stage = "execute"
	driveCtx, stopDrive := context.WithCancel(b.ctx)
	defer stopDrive()
	var value quickjs.Value
	if entry.Module {
		name, _ := json.Marshal(wrapperName)
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
		var waitErr error
		if r.awaitDefault {
			_, waitErr = r.defaultWait.Await(b.ctx)
		} else {
			_, waitErr = observed.Await(b.ctx)
		}
		return result, waitErr
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
	if b.exitRequested {
		return result, nil
	}
	if entry.Module && r.hasData {
		stage = "serialize"
		encoded, stringifyErr := r.stringify.CallContext(b.ctx, r.dataValue)
		if stringifyErr != nil {
			return result, fmt.Errorf("stdlib: evaluated result could not be serialized; side effects will not be replayed: %w", stringifyErr)
		}
		if !encoded.IsString() {
			return result, errors.New("stdlib: evaluated result could not be serialized; JSON.stringify returned no JSON text")
		}
		result.Data = json.RawMessage(encoded.String())
		stage = "execute"
		if err := r.loop.Run(driveCtx); err != nil {
			return result, err
		}
		if err := b.ctx.Err(); err != nil {
			return result, err
		}
		if err := b.failure(); err != nil {
			return result, err
		}
		if code, ok := lower.ExitCode(r.rt); ok {
			result.Code = code
		}
	}
	return result, nil
}

func (r *Runtime) installEntryWrapper(b *Bindings, entryName string) (string, error) {
	helperName := r.helperName
	for {
		wrapperName := helperName + "/entry"
		if helperName == entryName || wrapperName == entryName {
			helperName = fmt.Sprintf("noe:internal/jsexec/%d", nextRuntimeID.Add(1))
			continue
		}
		quotedEntry, _ := json.Marshal(entryName)
		quotedHelper, _ := json.Marshal(helperName)
		wrapperSource := "import {run,accept} from " + string(quotedHelper) + ";\nimport * as entry from " + string(quotedEntry) + ";\nawait run(entry,accept);\n"
		installed, err := b.installManagedModules(helperName, wrapperName, wrapperSource, map[string]any{
			"run": r.runDefault, "accept": r.acceptDefault,
		})
		if err != nil {
			return "", err
		}
		if installed {
			r.helperName = helperName
			return wrapperName, nil
		}
		helperName = fmt.Sprintf("noe:internal/jsexec/%d", nextRuntimeID.Add(1))
	}
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
		return Result{Code: 1, Diagnostics: diagnosticsFor(err, "setup")}, err
	}
	result, err = runtime.Run(source)
	closeErr := runtime.Close()
	if closeErr != nil {
		for _, cause := range errorLeaves(closeErr) {
			if err != nil && errors.Is(err, cause) {
				continue
			}
			result.Diagnostics = append(result.Diagnostics, diagnosticsFor(&executionStageError{stage: "cleanup", err: cause}, "cleanup")...)
		}
	}
	err = errors.Join(err, closeErr)
	if err != nil {
		result.Code = 1
	}
	return result, err
}
