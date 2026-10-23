package quickjs_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
)

func TestPreparedModuleLifecycleAndMetadata(t *testing.T) {
	rt := quickjs.New()
	t.Cleanup(func() { _ = rt.Close() })
	if _, err := rt.Eval(`globalThis.moduleRuns = []`); err != nil {
		t.Fatal(err)
	}
	if err := rt.SetModule("native", map[string]any{"value": 7}); err != nil {
		t.Fatal(err)
	}

	loads := 0
	rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		loads++
		if specifier != "./dep.mjs" || referrer != "/main.mjs" {
			return "", "", errors.New("unexpected request")
		}
		return `moduleRuns.push("dep"); export const dep = 40;`, "/dep.mjs", nil
	})

	inspected, err := rt.InspectModuleContext(t.Context(), "./dep.mjs", "/main.mjs")
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Specifier != "/dep.mjs" || inspected.Kind != quickjs.ModuleKindSource || !reflect.DeepEqual(inspected.Exports, []string{"dep"}) {
		t.Fatalf("inspected metadata = %#v", inspected)
	}
	assertModuleRuns(t, rt, "")

	mod, err := rt.PrepareModuleContext(t.Context(), "/main.mjs", `
		import {dep} from "./dep.mjs";
		moduleRuns.push("main");
		export const answer = dep + 2;
		export default answer;
		await Promise.resolve();
		export const late = "initialized";
	`)
	if err != nil {
		t.Fatal(err)
	}

	// The namespace exposes its shape while every lexical export remains in
	// its temporal dead zone. ExportNames must not read an export accessor.
	exports, err := mod.ExportNames()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(exports)
	if want := []string{"answer", "default", "late"}; !reflect.DeepEqual(exports, want) {
		t.Fatalf("exports = %v, want %v", exports, want)
	}
	all, err := rt.ModuleMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || loads != 1 {
		t.Fatalf("registered modules = %#v, loader calls = %d", all, loads)
	}
	var native *quickjs.ModuleMetadata
	for i := range all {
		if all[i].Specifier == "native" {
			native = &all[i]
			break
		}
	}
	if native == nil || native.Kind != quickjs.ModuleKindNative || !reflect.DeepEqual(native.Exports, []string{"value"}) {
		t.Fatalf("native module metadata = %#v", native)
	}
	assertModuleRuns(t, rt, "")

	promise, err := mod.EvaluateContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !promise.IsPromise() {
		t.Fatal("EvaluateContext did not return a Promise")
	}
	plain, err := rt.Eval(`({})`)
	if err != nil {
		t.Fatal(err)
	}
	if plain.IsPromise() {
		t.Fatal("ordinary object was identified as a Promise")
	}
	completion, err := promise.Await(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !completion.IsUndefined() {
		t.Fatalf("module completion = %v, want undefined", completion)
	}
	namespace, err := mod.Namespace()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"answer": "42", "default": "42", "late": "initialized"} {
		got, err := namespace.Get(name)
		if err != nil || got.String() != want {
			t.Errorf("namespace.%s = %v, %v; want %q", name, got, err, want)
		}
	}
	assertModuleRuns(t, rt, "dep main")

	// Re-evaluation uses the same linked modules and does not run either body
	// a second time.
	again, err := mod.EvaluateContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := again.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertModuleRuns(t, rt, "dep main")
}

func TestValueErrorFramesUseCapturedTraceWithoutGetters(t *testing.T) {
	rt := quickjs.New()
	_, err := rt.EvalFile("error-frames.js", `
globalThis.stackGetterCalls = 0;
function captureError() {
  const error = new Error("captured");
  Object.defineProperty(error, "stack", {configurable:true, get() {
    globalThis.stackGetterCalls++;
    throw new Error("stack getter must not run");
  }});
  globalThis.capturedError = error;
  const plain = {};
  Error.captureStackTrace(plain);
  globalThis.plainObject = plain;
}
captureError();`)
	if err != nil {
		rt.Close()
		t.Fatal(err)
	}
	captured, err := rt.Global().Get("capturedError")
	if err != nil {
		rt.Close()
		t.Fatal(err)
	}
	frames := captured.ErrorFrames()
	if len(frames) == 0 || frames[0].Source != "error-frames.js" || frames[0].Line == 0 {
		rt.Close()
		t.Fatalf("captured Error frames = %#v", frames)
	}
	plain, err := rt.Global().Get("plainObject")
	if err != nil {
		rt.Close()
		t.Fatal(err)
	}
	if frames := plain.ErrorFrames(); len(frames) != 0 {
		rt.Close()
		t.Fatalf("non-Error frames = %#v", frames)
	}
	getterCalls, err := rt.Global().Get("stackGetterCalls")
	if err != nil || getterCalls.Int() != 0 {
		rt.Close()
		t.Fatalf("stack getter calls=%v error=%v", getterCalls, err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if frames := captured.ErrorFrames(); len(frames) != 0 {
		t.Fatalf("closed runtime frames = %#v", frames)
	}
}

func TestPreparedModuleThenExportDoesNotAssimilateNamespace(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	if _, err := rt.Eval(`globalThis.thenRuns = 0`); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.PrepareModuleContext(t.Context(), "/then.mjs", `
		export function then(resolve) {
			thenRuns++;
			resolve("hijacked");
		}
		export default 42;
	`)
	if err != nil {
		t.Fatal(err)
	}
	promise, err := mod.EvaluateContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	completion, err := promise.Await(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !completion.IsUndefined() {
		t.Fatalf("module completion = %v, want undefined", completion)
	}
	runs, err := rt.Eval(`thenRuns`)
	if err != nil || runs.Int() != 0 {
		t.Fatalf("then export ran %v times, error %v", runs, err)
	}
	namespace, err := mod.Namespace()
	if err != nil {
		t.Fatal(err)
	}
	def, err := namespace.Get("default")
	if err != nil || def.Int() != 42 {
		t.Fatalf("namespace default = %v, %v; want 42", def, err)
	}
	if _, err := mod.AwaitContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	runs, err = rt.Eval(`thenRuns`)
	if err != nil || runs.Int() != 0 {
		t.Fatalf("then export ran after AwaitContext %v times, error %v", runs, err)
	}
}

func TestModuleDiagnosticsAndOwnership(t *testing.T) {
	t.Run("parse position", func(t *testing.T) {
		rt := quickjs.New()
		defer rt.Close()
		_, err := rt.PrepareModuleContext(t.Context(), "/syntax.mjs", "export const value = ;")
		var moduleErr *quickjs.ModuleError
		var syntaxErr *quickjs.SyntaxError
		if !errors.As(err, &moduleErr) || moduleErr.Phase != quickjs.ModulePhaseParse || moduleErr.Source != "/syntax.mjs" {
			t.Fatalf("module error = %#v", err)
		}
		if !errors.As(err, &syntaxErr) {
			t.Fatalf("error %T does not preserve SyntaxError: %v", err, err)
		}
		file, line, column := syntaxErr.Position()
		if file != "/syntax.mjs" || line != 1 || column == 0 {
			t.Fatalf("SyntaxError.Position() = %q:%d:%d", file, line, column)
		}
	})

	t.Run("missing export is link phase", func(t *testing.T) {
		rt := quickjs.New()
		defer rt.Close()
		rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
			return `export const present = 1;`, "/dep.mjs", nil
		})
		_, err := rt.PrepareModuleContext(t.Context(), "/main.mjs", `import {missing} from "./dep.mjs"; export {missing};`)
		var moduleErr *quickjs.ModuleError
		if !errors.As(err, &moduleErr) || moduleErr.Phase != quickjs.ModulePhaseLink {
			t.Fatalf("module error = %#v, want link phase", err)
		}
	})

	t.Run("resolve cause", func(t *testing.T) {
		rt := quickjs.New()
		defer rt.Close()
		cause := errors.New("loader sentinel")
		rt.SetModuleLoader(func(string, string) (string, string, error) { return "", "", cause })
		_, err := rt.PrepareModuleContext(t.Context(), "/main.mjs", `import "./missing.mjs";`)
		var moduleErr *quickjs.ModuleError
		if !errors.As(err, &moduleErr) || moduleErr.Phase != quickjs.ModulePhaseResolve || !errors.Is(err, cause) {
			t.Fatalf("module error = %#v, cause not preserved", err)
		}
	})

	t.Run("async resolve phase", func(t *testing.T) {
		rt := quickjs.New()
		defer rt.Close()
		cause := errors.New("async loader sentinel")
		rt.SetAsyncModuleLoader(func(ctx context.Context, req quickjs.ModuleRequest, done func(quickjs.LoadedModule, error)) {
			done(quickjs.LoadedModule{}, cause)
		})
		_, err := rt.PrepareModuleContext(t.Context(), "/main.mjs", `import "./missing.mjs";`)
		var moduleErr *quickjs.ModuleError
		if !errors.As(err, &moduleErr) || moduleErr.Phase != quickjs.ModulePhaseResolve || !errors.Is(err, cause) {
			t.Fatalf("async module error = %#v, cause not preserved", err)
		}
	})

	t.Run("evaluation cause and frames", func(t *testing.T) {
		rt := quickjs.New()
		defer rt.Close()
		mod, err := rt.PrepareModuleContext(t.Context(), "/throws.mjs", `function fail() { throw new Error("module boom"); } fail(); export const value = 1;`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = mod.AwaitContext(t.Context())
		var moduleErr *quickjs.ModuleError
		var jsErr *quickjs.Error
		if !errors.As(err, &moduleErr) || moduleErr.Phase != quickjs.ModulePhaseEvaluate || !errors.As(err, &jsErr) {
			t.Fatalf("evaluation error = %#v; want ModuleError and JS Error", err)
		}
		if frames := jsErr.Frames(); len(frames) == 0 || frames[0].Source != "/throws.mjs" {
			t.Fatalf("JavaScript frames = %#v", frames)
		}
	})

	t.Run("Go cause through module promise", func(t *testing.T) {
		rt := quickjs.New()
		defer rt.Close()
		cause := errors.New("host error sentinel")
		if err := rt.Set("failModule", func() error { return cause }); err != nil {
			t.Fatal(err)
		}
		mod, err := rt.PrepareModuleContext(t.Context(), "/host-throws.mjs", `failModule(); export const value = 1;`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = mod.AwaitContext(t.Context())
		var moduleErr *quickjs.ModuleError
		if !errors.As(err, &moduleErr) || moduleErr.Phase != quickjs.ModulePhaseEvaluate || !errors.Is(err, cause) {
			t.Fatalf("Go cause = %#v, want evaluate ModuleError wrapping sentinel", err)
		}
	})

	t.Run("owner and closed runtime", func(t *testing.T) {
		owner := quickjs.New()
		mod, err := owner.PrepareModuleContext(t.Context(), "/owned.mjs", `export const value = 1;`)
		if err != nil {
			owner.Close()
			t.Fatal(err)
		}
		other := quickjs.New()
		defer other.Close()
		if _, err := other.EvaluateModuleContext(t.Context(), mod); !errors.Is(err, quickjs.ErrModuleOwner) {
			t.Fatalf("foreign owner error = %v", err)
		}
		owner.Close()
		if _, err := mod.EvaluateContext(t.Context()); !errors.Is(err, quickjs.ErrClosed) {
			t.Fatalf("closed owner error = %v", err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		for _, src := range []string{
			`while (true) {} export const never = 1;`,
			`await Promise.resolve(); while (true) {} export default 1;`,
		} {
			rt := quickjs.New()
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			_, err := rt.EvalModuleContext(ctx, "/loop.mjs", src)
			cancel()
			_ = rt.Close()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("cancelled evaluation of %q = %T (%#v)", src, err, err)
			}
		}
	})
}

func TestPreparedModuleCycleAndTopLevelAwait(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	if _, err := rt.Eval(`globalThis.moduleRuns = []`); err != nil {
		t.Fatal(err)
	}
	rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		switch specifier {
		case "./a.mjs":
			return `import {b} from "./b.mjs"; moduleRuns.push("a"); export const a = "a"; export const joined = await Promise.resolve(a + b);`, "/a.mjs", nil
		case "./b.mjs":
			return `import {a} from "./a.mjs"; moduleRuns.push("b"); export const b = "b";`, "/b.mjs", nil
		default:
			return "", "", errors.New("unexpected module " + specifier)
		}
	})
	mod, err := rt.PrepareModuleContext(t.Context(), "/main.mjs", `import {joined} from "./a.mjs"; moduleRuns.push("main"); export {joined};`)
	if err != nil {
		t.Fatal(err)
	}
	assertModuleRuns(t, rt, "")
	ns, err := mod.AwaitContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	joined, err := ns.Get("joined")
	if err != nil || joined.String() != "ab" {
		t.Fatalf("joined = %v, %v; want ab", joined, err)
	}
	assertModuleRuns(t, rt, "b a main")
}

func assertModuleRuns(t *testing.T, rt *quickjs.Runtime, want string) {
	t.Helper()
	got, err := rt.Eval(`moduleRuns.join(" ")`)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != want {
		t.Fatalf("module body order = %q, want %q", got.String(), want)
	}
}

func TestPreparedModuleDependencyLinkSource(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		switch specifier {
		case "./a.mjs":
			return `import {missing} from "./b.mjs"; export {missing};`, "/a.mjs", nil
		case "./b.mjs":
			return `export const present = 1;`, "/b.mjs", nil
		default:
			return "", "", errors.New("unexpected request")
		}
	})
	_, err := rt.PrepareModuleContext(t.Context(), "/main.mjs", `import "./a.mjs";`)
	var moduleErr *quickjs.ModuleError
	if !errors.As(err, &moduleErr) || moduleErr.Phase != quickjs.ModulePhaseLink {
		t.Fatalf("expected link diagnostic, got %T: %v", err, err)
	}
	if moduleErr.Source != "/a.mjs" {
		t.Fatalf("link source = %q, want failing importer /a.mjs", moduleErr.Source)
	}
}
