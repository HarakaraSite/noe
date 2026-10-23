package jsexec

import (
	"context"
	"errors"

	lower "forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// CheckResult describes the result of parsing, compiling and linking a static
// module graph. A successful check reaches the link stage without evaluating
// any module body.
type CheckResult struct {
	OK          bool             `json:"ok"`
	Stage       string           `json:"stage"`
	Entry       string           `json:"entry"`
	Diagnostics []DiagnosticInfo `json:"diagnostics,omitempty"`
}

// Check compiles and links source and its static dependencies in a fresh
// runtime. It never evaluates JavaScript module bodies.
func Check(ctx context.Context, source Source, profile Profile) (result CheckResult, err error) {
	return checkWithMode(ctx, source, profile, false)
}

func checkWithMode(ctx context.Context, source Source, profile Profile, worker bool) (result CheckResult, err error) {
	result = CheckResult{Stage: "prepare", Entry: source.Name}
	if ctx == nil {
		err = errors.New("stdlib: check context is required")
		result.Diagnostics = diagnosticInfos(err)
		return result, err
	}
	if err = ctx.Err(); err != nil {
		result.Diagnostics = fillDiagnosticPhase(diagnosticInfos(err), result.Stage)
		return result, err
	}

	rt := quickjs.New()
	loop := lower.NewLoop(rt)
	rt.OnClose(loop.Close)

	inspectProfile := profile
	inspectProfile.Initialize = profile.InspectInitialize
	inspectProfile.InspectInitialize = nil
	bindings, err := attach(rt, loop, inspectProfile, worker, nil, ctx)
	if err != nil {
		err = retainException(err)
		result.Diagnostics = fillDiagnosticPhase(diagnosticInfos(err), result.Stage)
		rt.Close()
		return result, err
	}

	source.Module = true
	entry, prepareErr := bindings.prepare(source, true)
	if prepareErr != nil {
		err = bindings.retainException(prepareErr)
	} else {
		result.Entry = entry.Name
		result.Stage = "prepare"
		_, err = rt.PrepareModuleContext(bindings.ctx, entry.Name, entry.Code)
		if err != nil {
			err = bindings.retainException(err)
		} else {
			result.OK = true
			result.Stage = "link"
		}
	}
	if err != nil {
		result.OK = false
		result.Diagnostics = fillDiagnosticPhase(diagnosticInfos(err), result.Stage)
		if len(result.Diagnostics) > 0 && result.Diagnostics[0].Phase != "" {
			result.Stage = result.Diagnostics[0].Phase
		}
	}
	if contextErr := ctx.Err(); contextErr != nil {
		if err == nil {
			err = contextErr
		} else if !errors.Is(err, contextErr) {
			err = errors.Join(err, contextErr)
		}
		result.OK = false
		result.Diagnostics = fillDiagnosticPhase(diagnosticInfos(err), result.Stage)
	}

	cleanupErr := bindings.Close()
	loop.Close()
	rt.Close()
	if cleanupErr != nil {
		if err == nil {
			result.Stage = "cleanup"
		}
		cleanupDiagnostics := fillDiagnosticPhase(diagnosticInfos(cleanupErr), "cleanup")
		err = errors.Join(err, cleanupErr)
		result.OK = false
		result.Diagnostics = append(result.Diagnostics, cleanupDiagnostics...)
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func (b *Bindings) installRuntimeModule(profile Profile) error {
	return b.rt.SetModule("noe:runtime", map[string]any{
		"check": func(code quickjs.Value, extra ...quickjs.Value) (CheckResult, error) {
			if len(extra) > 0 {
				return CheckResult{}, b.rt.ThrowTypeError("check expects exactly one source string")
			}
			if !code.IsString() {
				return CheckResult{}, b.rt.ThrowTypeError("check source must be a string")
			}
			if err := b.ctx.Err(); err != nil {
				return CheckResult{}, err
			}
			result, err := checkWithMode(b.ctx, Source{
				Name: "[runtime check]", Code: code.String(), Module: true, Virtual: true,
			}, profile, b.worker)
			if err == nil || (isNormalModuleCheckFailure(result, err) && !isContextFailure(err)) {
				return result, nil
			}
			return result, err
		},
	})
}

func isNormalModuleCheckFailure(result CheckResult, err error) bool {
	if err == nil || isContextFailure(err) {
		return false
	}
	for _, diagnostic := range result.Diagnostics {
		switch diagnostic.Phase {
		case string(quickjs.ModulePhaseParse), string(quickjs.ModulePhaseCompile), string(quickjs.ModulePhaseResolve), string(quickjs.ModulePhaseLink):
			return true
		}
	}
	return false
}
