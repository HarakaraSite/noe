package stdlib

import (
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// builtinEvaluation controls how the larger built-in factories are loaded.
// RunProgram prepares per-realm literal roots eagerly, so runtimes with
// caller-selected resource limits keep the EvalFile installation path.
type builtinEvaluation struct {
	reusePrograms bool
}

type builtinFactory struct {
	name, source string
	compile      func() *quickjs.Program
}

// fixedBuiltin defines one package-owned script. QuickJS Programs contain no
// runtime values and are safe to share. Each run creates its own closures.
func fixedBuiltin(name, source string) builtinFactory {
	return builtinFactory{name, source, sync.OnceValue(func() *quickjs.Program {
		// If standard compilation fails, use the runtime's EvalFile so the
		// runtime's parsing mode and syntax diagnostics take precedence.
		program, _ := quickjs.Compile(name, source)
		return program
	})}
}

func (mode builtinEvaluation) withHost(rt *quickjs.Runtime, factory builtinFactory, host quickjs.Value) (quickjs.Value, error) {
	if mode.reusePrograms {
		if program := factory.compile(); program != nil {
			// RunProgram selects the runtime's Node quirks and recompiles
			// for its debugger when enabled.
			fn, err := rt.RunProgram(program)
			if err != nil {
				return quickjs.Value{}, err
			}
			return callHostFactory(rt, fn, host)
		}
	}
	return evalWithHost(rt, factory.name, factory.source, host)
}
