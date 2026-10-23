package jsexec

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestCheckUsesInspectInitializerAndDoesNotEvaluateModules(t *testing.T) {
	var ordinaryInitializers, inspectInitializers, cleanups, loaderCalls, evaluations atomic.Int32
	profile := Profile{
		Initialize: func(context.Context, *Bindings) (func(), error) {
			ordinaryInitializers.Add(1)
			return nil, nil
		},
		InspectInitialize: func(_ context.Context, b *Bindings) (func(), error) {
			inspectInitializers.Add(1)
			err := b.Runtime().SetModule("fixture:counter", map[string]any{
				"mark": func() { evaluations.Add(1) },
			})
			return func() { cleanups.Add(1) }, err
		},
		NewLoader: func(context.Context, Origin) (quickjs.ModuleLoader, error) {
			return func(specifier, _ string) (string, string, error) {
				loaderCalls.Add(1)
				if specifier != "./dep.mjs" {
					return "", "", errors.New("unexpected module " + specifier)
				}
				return `import {mark} from "fixture:counter";mark();export const value=42;`, "/dep.mjs", nil
			}, nil
		},
	}

	result, err := Check(t.Context(), Source{
		Name: "[check]", Virtual: true,
		Code: `import {mark} from "fixture:counter";import {value} from "./dep.mjs";mark();export const answer=value;`,
	}, profile)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.OK || result.Stage != "link" || result.Entry != "[check]" {
		t.Fatalf("Check result = %#v", result)
	}
	if ordinaryInitializers.Load() != 0 || inspectInitializers.Load() != 1 || cleanups.Load() != 1 {
		t.Fatalf("ordinary/inspect/cleanup counts = %d/%d/%d", ordinaryInitializers.Load(), inspectInitializers.Load(), cleanups.Load())
	}
	if loaderCalls.Load() != 1 || evaluations.Load() != 0 {
		t.Fatalf("loader/evaluation calls = %d/%d", loaderCalls.Load(), evaluations.Load())
	}
}

func TestCheckReturnsStructuredParseAndResolveFailures(t *testing.T) {
	t.Run("parse", func(t *testing.T) {
		result, err := Check(t.Context(), Source{
			Name: "[syntax]", Virtual: true, Code: "\nexport const value = ;",
		}, Profile{})
		if err == nil || result.OK || result.Stage != "parse" || len(result.Diagnostics) != 1 {
			t.Fatalf("check result=%#v error=%v", result, err)
		}
		diagnostic := result.Diagnostics[0]
		if diagnostic.Code != codeJSSyntax || diagnostic.Phase != "parse" || diagnostic.Source != "[syntax]" || diagnostic.Line != 2 || diagnostic.Column == 0 {
			t.Fatalf("parse diagnostic = %#v", diagnostic)
		}
	})

	t.Run("resolve", func(t *testing.T) {
		profile := Profile{NewLoader: func(context.Context, Origin) (quickjs.ModuleLoader, error) {
			return func(specifier, _ string) (string, string, error) {
				return "", "", errors.New("fixture missing " + specifier)
			}, nil
		}}
		result, err := Check(t.Context(), Source{
			Name: "[resolve]", Virtual: true, Code: `import "./missing.mjs"; export const value=1;`,
		}, profile)
		if err == nil || result.OK || result.Stage != "resolve" || len(result.Diagnostics) != 1 {
			t.Fatalf("check result=%#v error=%v", result, err)
		}
		diagnostic := result.Diagnostics[0]
		if diagnostic.Code != codeJSModule || diagnostic.Phase != "resolve" || diagnostic.Source != "[resolve]" {
			t.Fatalf("resolve diagnostic = %#v", diagnostic)
		}
	})
}

func TestCheckCancellationStopsLoaderAndRunsInspectionCleanup(t *testing.T) {
	var cleanups atomic.Int32
	profile := Profile{
		InspectInitialize: func(context.Context, *Bindings) (func(), error) {
			return func() { cleanups.Add(1) }, nil
		},
		NewLoader: func(ctx context.Context, _ Origin) (quickjs.ModuleLoader, error) {
			return func(string, string) (string, string, error) {
				<-ctx.Done()
				return "", "", ctx.Err()
			}, nil
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	result, err := Check(ctx, Source{
		Name: "[cancel-check]", Virtual: true, Code: `import "./blocked.mjs";`,
	}, profile)
	if !errors.Is(err, context.DeadlineExceeded) || result.OK || cleanups.Load() != 1 {
		t.Fatalf("check result=%#v error=%v cleanups=%d", result, err, cleanups.Load())
	}
	if len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != codeJSTimeout {
		t.Fatalf("cancellation diagnostics = %#v", result.Diagnostics)
	}
}

func TestRuntimeCheckUsesSeparateVMAndReturnsNormalFailures(t *testing.T) {
	var ordinaryInitializers, inspectInitializers, cleanups atomic.Int32
	profile := Profile{
		Initialize: func(_ context.Context, b *Bindings) (func(), error) {
			ordinaryInitializers.Add(1)
			return nil, b.Runtime().Set("initializedMain", true)
		},
		InspectInitialize: func(_ context.Context, b *Bindings) (func(), error) {
			inspectInitializers.Add(1)
			err := b.Runtime().Set("inspectionOnly", true)
			return func() { cleanups.Add(1) }, err
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result, err := Execute(ctx, Source{
		Name: "[runtime-check]", Virtual: true, Module: true,
		Code: `import {check} from "noe:runtime";
const valid=check("globalThis.checkedBodyRan=true;export const value=1;");
const invalid=check("export const value=;");
export default {valid:valid.ok,stage:valid.stage,invalid:invalid.ok,errorCode:invalid.diagnostics[0].code,checkedBody:typeof globalThis.checkedBodyRan,mainInit:initializedMain};`,
	}, profile)
	if err != nil {
		t.Fatalf("Execute: %v (%+v)", err, result)
	}
	if result.Phase != "complete" || string(result.Data) != `{"valid":true,"stage":"link","invalid":false,"errorCode":"JS_SYNTAX","checkedBody":"undefined","mainInit":true}` {
		t.Fatalf("runtime.check result=%#v data=%s", result, result.Data)
	}
	if ordinaryInitializers.Load() != 1 || inspectInitializers.Load() != 2 || cleanups.Load() != 2 {
		t.Fatalf("ordinary/inspect/cleanup counts = %d/%d/%d", ordinaryInitializers.Load(), inspectInitializers.Load(), cleanups.Load())
	}
}

func TestRuntimeCheckPreservesWorkerScope(t *testing.T) {
	var parentInitializers, workerInitializers, parentOrigins, workerOrigins atomic.Int32
	profile := Profile{
		Modules: Modules{Workers: &WorkerOptions{}},
		InspectInitialize: func(_ context.Context, b *Bindings) (func(), error) {
			if b.Worker() {
				workerInitializers.Add(1)
				return nil, b.Runtime().SetModule("fixture:worker-only", map[string]any{"value": "worker"})
			}
			parentInitializers.Add(1)
			return nil, b.Runtime().SetModule("fixture:parent-only", map[string]any{"value": "parent"})
		},
		NewLoader: func(_ context.Context, origin Origin) (quickjs.ModuleLoader, error) {
			if origin.Entry == "[runtime check]" {
				if origin.Worker {
					workerOrigins.Add(1)
				} else {
					parentOrigins.Add(1)
				}
			}
			return func(specifier, _ string) (string, string, error) {
				return "", "", errors.New("unexpected module " + specifier)
			}, nil
		},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	result, err := Execute(ctx, Source{
		Name: "[runtime-check-worker-scope]", Virtual: true, Module: true,
		Code: `import {Worker} from "node:worker_threads";
import {check} from "noe:runtime";
const parentValid=check('import {value} from "fixture:parent-only";export const result=value;');
const parentWrong=check('import {value} from "fixture:worker-only";export const result=value;');
const worker=new Worker(` + "`" + `import {parentPort} from "node:worker_threads";
import {check} from "noe:runtime";
const valid=check('import {value} from "fixture:worker-only";export const result=value;');
const wrong=check('import {value} from "fixture:parent-only";export const result=value;');
parentPort.postMessage({valid:valid.ok,wrong:wrong.ok,stage:valid.stage});` + "`" + `,{eval:true,type:"module"});
const workerResult=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});
if(!parentValid.ok||parentWrong.ok||!workerResult.valid||workerResult.wrong||workerResult.stage!=="link")throw new Error(JSON.stringify({parentValid,parentWrong,workerResult}));
export default {parentValid:parentValid.ok,parentWrong:parentWrong.ok,workerValid:workerResult.valid,workerWrong:workerResult.wrong};`,
	}, profile)
	if err != nil || result.Code != 0 || string(result.Data) != `{"parentValid":true,"parentWrong":false,"workerValid":true,"workerWrong":false}` {
		t.Fatalf("result=%#v data=%s error=%v", result, result.Data, err)
	}
	if parentInitializers.Load() != 2 || workerInitializers.Load() != 2 || parentOrigins.Load() != 2 || workerOrigins.Load() != 2 {
		t.Fatalf("inspect initializers parent/worker=%d/%d; loader origins parent/worker=%d/%d", parentInitializers.Load(), workerInitializers.Load(), parentOrigins.Load(), workerOrigins.Load())
	}
}
