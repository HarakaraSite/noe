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
	h            *host
	inputNumber  int
	lexicalNames map[string]struct{}
}

// Evaluation separates a recoverable diagnostic from session termination.
type Evaluation struct {
	Output string
	Err    error
	Exit   bool
	Code   int
}

// TerminationError reports that a Session operation ended the runtime or
// found it already closed. Cause is nil for an explicit noe.exit request.
type TerminationError struct {
	Code  int
	Cause error
}

func (e *TerminationError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("runtime terminated with exit code %d: %v", e.Code, e.Cause)
	}
	return fmt.Sprintf("runtime terminated with exit code %d", e.Code)
}

func (e *TerminationError) Unwrap() error { return e.Cause }

// NewSession installs the host APIs with no arguments and no readStdin API.
func NewSession(opts Options) (*Session, error) {
	opts.Args = nil
	opts.Stdin = nil // stdin supplies REPL source, not script data
	h, err := newHost(opts)
	if err != nil {
		return nil, err
	}
	undefined, err := h.rt.Get("undefined")
	if err != nil {
		h.rt.Close()
		return nil, err
	}
	if err := h.rt.Set("_", undefined); err != nil {
		h.rt.Close()
		return nil, err
	}
	h.installLoader(nil)
	return &Session{h: h, lexicalNames: make(map[string]struct{})}, nil
}

// Close releases the session and cancels its host work on the owner goroutine.
func (s *Session) Close() { s.h.rt.Close() }

// Eval evaluates one input without discarding declarations or prior mutations.
func (s *Session) Eval(ctx context.Context, input string) Evaluation {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.h.inputContext = ctx
	defer func() { s.h.inputContext = nil }()

	s.inputNumber++
	name := filepath.Join(s.h.cwd, fmt.Sprintf("<repl:%d>.js", s.inputNumber))
	value, err := s.evalInput(ctx, name, input)
	if err == nil {
		for _, declared := range topLevelLexicalNames(input) {
			s.lexicalNames[declared] = struct{}{}
		}
	}
	result := s.finish(ctx, name, value, err)
	var failedLoop *loopFailure
	if !result.Exit && errors.As(result.Err, &failedLoop) {
		cancel()
		return s.finishCancelledInput(ctx, name, result)
	}
	if result.Exit || ctx.Err() == nil || result.Err == nil || !errors.Is(result.Err, ctx.Err()) {
		return result
	}
	return s.finishCancelledInput(ctx, name, result)
}

func (s *Session) finishCancelledInput(ctx context.Context, name string, result Evaluation) Evaluation {
	h := s.h
	// First make the engine observe an abort in a script turn. This drops
	// Promise reactions left queued by the canceled input before drainCancelled
	// runs host completion callbacks with a background context.
	cleanupErr := h.discardCancelledJobs()
	if cleanupErr != nil {
		cleanupErr = errors.Join(cleanupErr, h.takeUnhandled())
		if h.exitRequested && errors.Is(cleanupErr, quickjs.ErrClosed) {
			return Evaluation{Exit: true, Code: h.exitCode}
		}
		return Evaluation{Err: fmt.Errorf("%s: cancellation cleanup: %w", name, cleanupErr), Exit: true, Code: 1}
	}
	cleanupErr = h.drainCancelled(ctx)
	cleanupErr = errors.Join(cleanupErr, h.takeUnhandled())
	if cleanupErr != nil && sessionFatal(cleanupErr) {
		if h.exitRequested && errors.Is(cleanupErr, quickjs.ErrClosed) {
			return Evaluation{Exit: true, Code: h.exitCode}
		}
		return Evaluation{Err: fmt.Errorf("%s: %w", name, cleanupErr), Exit: true, Code: 1}
	}

	err := result.Err
	if err == nil {
		err = fmt.Errorf("%s: %w", name, ctx.Err())
	}
	if cleanupErr != nil {
		err = errors.Join(err, cleanupErr)
	}
	return Evaluation{Err: err, Exit: false, Code: 1}
}

func (s *Session) finish(ctx context.Context, name string, value quickjs.Value, err error) Evaluation {
	h := s.h
	if err != nil {
		h.loop.stopSources()
	}
	var failedLoop *loopFailure
	if !sessionFatal(err) && !h.exitRequested && !errors.As(err, &failedLoop) {
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
	if err != nil {
		h.loop.stopSources()
	}
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
	if ctx.Err() != nil {
		return Evaluation{Err: ctx.Err(), Exit: false, Code: 1}
	}
	if err := h.rt.Set("_", value); err != nil {
		return Evaluation{Err: fmt.Errorf("%s: %w", name, err), Exit: sessionFatal(err), Code: 1}
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
	value, err := s.h.loop.await(ctx, p)
	if err != nil || form.binding == "" {
		return value, err
	}
	key := fmt.Sprintf("\x00noe.repl.%d", s.inputNumber)
	if err := s.h.rt.Global().Set(key, value); err != nil {
		return quickjs.Value{}, err
	}
	literal, _ := json.Marshal(key)
	defer s.h.rt.EvalFileContext(context.Background(), "<noe-repl-cleanup>", "delete this["+string(literal)+"]")
	commit := form.binding + " = this[" + string(literal) + "]"
	return s.h.rt.EvalFileContext(ctx, name, commit)
}

func sessionFatal(err error) bool {
	return errors.Is(err, quickjs.ErrInternal) || errors.Is(err, quickjs.ErrClosed)
}
