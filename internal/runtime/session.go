package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// Session owns one runtime for repeated REPL input. Use it on one goroutine.
type Session struct {
	h           *host
	inputNumber int
}

// Evaluation separates a recoverable diagnostic from session termination.
type Evaluation struct {
	Output string
	Err    error
	Exit   bool
	Code   int
}

// NewSession installs the same host APIs as file execution, with no arguments.
func NewSession(opts Options) (*Session, error) {
	opts.Args = nil
	h, err := newHost(opts)
	if err != nil {
		return nil, err
	}
	h.installLoader(nil)
	return &Session{h: h}, nil
}

// Close releases the session and cancels its host work on the owner goroutine.
func (s *Session) Close() { s.h.rt.Close() }

// Eval evaluates one input without discarding declarations or prior mutations.
func (s *Session) Eval(ctx context.Context, input string) Evaluation {
	s.inputNumber++
	name := filepath.Join(s.h.cwd, fmt.Sprintf("<repl:%d>.js", s.inputNumber))
	value, err := s.evalInput(ctx, name, input)
	return s.finish(ctx, name, value, err)
}

func (s *Session) finish(ctx context.Context, name string, value quickjs.Value, err error) Evaluation {
	h := s.h
	if !sessionFatal(err) && !h.exitRequested {
		err = errors.Join(err, h.drain(ctx))
	}
	if h.exitRequested && errors.Is(err, quickjs.ErrClosed) {
		h.takeUnhandled()
		return Evaluation{Exit: true, Code: h.exitCode}
	}
	err = errors.Join(err, h.takeUnhandled())
	if err != nil {
		return Evaluation{Err: fmt.Errorf("%s: %w", name, err), Exit: sessionFatal(err), Code: 1}
	}
	if value.IsUndefined() {
		return Evaluation{}
	}
	formatted, err := h.inspect.CallContext(ctx, value)
	if !sessionFatal(err) && !h.exitRequested {
		err = errors.Join(err, h.drain(ctx))
	}
	err = errors.Join(err, h.takeUnhandled())
	if err != nil {
		if h.exitRequested && errors.Is(err, quickjs.ErrClosed) {
			return Evaluation{Exit: true, Code: h.exitCode}
		}
		return Evaluation{Err: err, Exit: sessionFatal(err), Code: 1}
	}
	return Evaluation{Output: strings.TrimSuffix(formatted.String(), "\n")}
}

func (s *Session) evalInput(ctx context.Context, name, input string) (quickjs.Value, error) {
	form, err := asyncInput(s.h.rt, input)
	if err != nil {
		return quickjs.Value{}, err
	}
	if form == nil {
		return s.h.rt.EvalFileContext(ctx, name, input)
	}
	code := `(() => { const p = (async () => (` + "\n" + form.expression + "\n" + `))(); p.catch(() => {}); return p; })()`
	p, err := s.h.rt.EvalFileContext(ctx, name, code)
	if err != nil {
		return quickjs.Value{}, err
	}
	value, err := p.Await(ctx)
	if err != nil || form.binding == "" {
		return value, err
	}
	key := fmt.Sprintf("\x00noe.repl.%d", s.inputNumber)
	if err := s.h.rt.Global().Set(key, value); err != nil {
		return quickjs.Value{}, err
	}
	literal, _ := json.Marshal(key)
	defer s.h.rt.EvalFileContext(ctx, "<noe-repl-cleanup>", "delete this["+string(literal)+"]")
	commit := form.binding + " = this[" + string(literal) + "]"
	return s.h.rt.EvalFileContext(ctx, name, commit)
}

func sessionFatal(err error) bool {
	return errors.Is(err, quickjs.ErrInternal) || errors.Is(err, quickjs.ErrClosed) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
