package stdlib

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	"forge.harakara.site/littleisland/noe/v2/internal/commandenv"
	"forge.harakara.site/littleisland/noe/v2/internal/process"
)

// Run describes the programs a script may start.
//
// This is the largest capability there is: a script that can run a program can
// do anything the user can, whatever else it was refused. The zero value allows
// nothing, and Allow decides for every attempt:
//
//	stdlib.Commands(rt, &stdlib.Run{
//	    Loop:  loop,
//	    Allow: func(name string, args []string) error {
//	        if name != "git" {
//	            return fmt.Errorf("only git may be run")
//	        }
//	        return nil
//	    },
//	})
type Run struct {
	// Loop is where the result is delivered for the promise form. Without one
	// the work is done before the call returns.
	Loop *Loop
	// Allow is asked about every program before it is started. Nil refuses
	// everything, which is what a zero value means.
	Allow func(name string, args []string) error
	// Dir is where a program runs when the caller does not say. Empty means
	// the process's own directory.
	Dir string
	// ResolveDirectory maps a script-visible cwd override to its physical
	// directory. Profile supplies the shared filesystem backend mapping.
	ResolveDirectory func(string) (string, error)
	// Supervisor selects an explicit POSIX process supervisor. Empty starts
	// commands directly; Windows always uses its Job Object backend.
	Supervisor string
	// Env is the environment a program is given. Nil means none at all, which
	// is what a script that was not given the environment should pass on.
	Env map[string]string
	// Timeout stops a program that runs too long. Zero means no limit.
	Timeout time.Duration
	// MaxOutputBytes caps what is captured from a program. Zero means 32 MB.
	MaxOutputBytes int
}

// Commands installs the "child_process" module.
//
// What is there is what can be done without streams: a program is started, it
// runs to completion, and its output comes back whole.
//
//	import {execFileSync, execFile} from "child_process"
//	const branch = execFileSync("git", ["branch", "--show-current"]).trim()
//	const {stdout} = await execFile("git", ["status", "--short"])
func Commands(rt *quickjs.Runtime, cfg *Run) error {
	if cfg == nil {
		cfg = &Run{}
	}
	r := &runner{rt: rt, cfg: cfg}

	exports := map[string]any{
		"execFileSync": r.execFileSync,
		"spawnSync":    r.spawnSync,
		"execFile":     r.execFile,
		"exec":         r.execShell,
		"execSync":     r.execShellSync,
	}
	exports["default"] = copyExports(exports)
	return setNodeModule(rt, "child_process", exports)
}

type runner struct {
	rt  *quickjs.Runtime
	cfg *Run
}

// result is a finished program.
type result struct {
	stdout []byte
	stderr []byte
	code   int
	signal string
	err    error
}

// start runs a program to completion. It touches no JavaScript value, so it
// may run on another goroutine.
func (r *runner) start(name string, args []string, dir string, env map[string]string, input []byte) *result {
	prepared, err := commandenv.Prepare(r.commandConfig(), name, args, dir, env)
	if err != nil {
		return &result{err: err}
	}
	return r.startPrepared(prepared, name, input)
}

func (r *runner) startShell(command string, dir string, env map[string]string, input []byte) *result {
	prepared, err := commandenv.Shell(r.commandConfig(), command, dir, env)
	if err != nil {
		return &result{err: err}
	}
	return r.startPrepared(prepared, prepared.Command, input)
}

func (r *runner) commandConfig() commandenv.Config {
	return commandenv.Config{
		Cwd: r.cfg.Dir, Env: r.cfg.Env, Allow: r.cfg.Allow,
		Timeout: r.cfg.Timeout, ResolveDirectory: r.cfg.ResolveDirectory,
		Supervisor: r.cfg.Supervisor,
	}
}

func (r *runner) startPrepared(prepared commandenv.Prepared, displayName string, input []byte) *result {
	limit := r.cfg.MaxOutputBytes
	if limit == 0 {
		limit = 32 << 20
	}
	var out, errOut cappedBuffer
	out.limit, errOut.limit = limit, limit
	ownerCtx := loopContext(r.cfg.Loop)
	ctx := ownerCtx
	cancel := func() {}
	if r.cfg.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ownerCtx, r.cfg.Timeout)
	}
	defer cancel()

	child, err := process.Start(ctx, process.Config{
		Command:    prepared.Command,
		Args:       prepared.Args,
		Dir:        prepared.Dir,
		Env:        commandenv.EnvList(prepared.Env),
		Supervisor: r.cfg.Supervisor,
	})
	if err != nil {
		return commandStartError(displayName, r.cfg.Timeout, ownerCtx.Err(), ctx.Err(), err)
	}

	var pumps sync.WaitGroup
	outputErrors := make(chan error, 1)
	var outputErrorOnce sync.Once
	rememberOutputError := func(name string, err error) {
		if err == nil || errors.Is(err, io.EOF) || ctx.Err() != nil {
			return
		}
		outputErrorOnce.Do(func() {
			outputErrors <- fmt.Errorf("capture command %s: %w", name, err)
		})
	}
	pumps.Add(3)
	go func() {
		defer pumps.Done()
		_, err := io.Copy(&out, child.Stdout)
		rememberOutputError("stdout", err)
		_ = child.Stdout.Close()
	}()
	go func() {
		defer pumps.Done()
		_, err := io.Copy(&errOut, child.Stderr)
		rememberOutputError("stderr", err)
		_ = child.Stderr.Close()
	}()
	go func() {
		defer pumps.Done()
		if len(input) != 0 {
			_, _ = io.Copy(child.Stdin, bytes.NewReader(input))
		}
		_ = child.Stdin.Close()
	}()

	type waitResult struct {
		result process.Result
		err    error
	}
	waitDone := make(chan waitResult, 1)
	go func() {
		result, err := child.Wait()
		waitDone <- waitResult{result: result, err: err}
	}()
	pumpsDone := make(chan struct{})
	go func() {
		pumps.Wait()
		close(pumpsDone)
	}()

	var childResult process.Result
	var waitErr, captureErr, closeErr error
	var waited, joinedPumps, closing bool
	ctxDone := ctx.Done()
	pumpDone := (<-chan struct{})(pumpsDone)
	for !waited || !joinedPumps {
		select {
		case waitedResult := <-waitDone:
			childResult, waitErr, waited = waitedResult.result, waitedResult.err, true
		case err := <-outputErrors:
			captureErr = errors.Join(captureErr, err)
			if !closing {
				// A failed capture cannot leave a live child blocked on a full
				// pipe. Close the owned scope without canceling the owner context.
				closing = true
				closeErr = errors.Join(closeErr, child.Close())
			}
		case <-ctxDone:
			ctxDone = nil
			if !closing {
				// Owner cancellation and timeout close the child pipes to release
				// both stream pumps before this operation joins them.
				closing = true
				closeErr = errors.Join(closeErr, child.Close())
			}
		case <-pumpDone:
			pumpDone = nil
			joinedPumps = true
		}
	}
	// A capture error can become ready alongside the final pump notification.
	// Observe it before deciding whether the normally completed child needs
	// explicit cleanup.
	select {
	case err := <-outputErrors:
		captureErr = errors.Join(captureErr, err)
	default:
	}
	if captureErr != nil && !closing {
		closing = true
		closeErr = errors.Join(closeErr, child.Close())
	}
	if !closing {
		// Normal completion drains stdout/stderr before Close so buffered output
		// remains available to the caller.
		closeErr = child.Close()
	}
	ownerErr, commandErr := ownerCtx.Err(), ctx.Err()

	res := &result{stdout: out.Bytes(), stderr: errOut.Bytes()}
	if childResult.Code != nil {
		res.code = *childResult.Code
	} else {
		res.code = -1
	}
	res.signal = childResult.Signal

	if captureErr != nil {
		res.code = -1
		res.err = errors.Join(captureErr, waitErr, closeErr)
		return res
	}
	if ownerErr != nil {
		// Preserve runtime cancellation as a context error; the sync path uses
		// this to avoid turning owner shutdown into a catchable JS exception.
		res.err = fmt.Errorf("%s was stopped: the runtime was closed: %w", displayName, ownerErr)
		return res
	}
	if errors.Is(commandErr, context.DeadlineExceeded) {
		res.code = -1
		res.err = fmt.Errorf("%s took longer than %s", displayName, r.cfg.Timeout)
		return res
	}

	if err := errors.Join(waitErr, closeErr); err != nil {
		res.code = -1
		res.err = err
		return res
	}
	if res.code != 0 || res.signal != "" {
		res.err = fmt.Errorf("%s exited with %d", displayName, res.code)
	}
	return res
}

func commandStartError(name string, timeout time.Duration, ownerErr, commandErr, startErr error) *result {
	res := &result{code: -1}
	if ownerErr != nil {
		res.err = fmt.Errorf("%s was stopped: the runtime was closed: %w", name, ownerErr)
	} else if errors.Is(commandErr, context.DeadlineExceeded) {
		res.err = fmt.Errorf("%s took longer than %s", name, timeout)
	} else {
		res.code = 0
		res.err = startErr
	}
	return res
}

// execFileSync runs a program and returns what it wrote, failing if it did.
func (r *runner) execFileSync(name string, argv quickjs.Value, opts quickjs.Value, extra ...string) (string, error) {
	args := append(stringsOf(argv), extra...)
	res := r.start(name, args, optionString(opts, "cwd"), optionEnv(opts), optionInput(opts))
	return r.execSyncResult(res)
}

func (r *runner) execShellSync(command string, opts quickjs.Value) (string, error) {
	res := r.startShell(command, optionString(opts, "cwd"), optionEnv(opts), optionInput(opts))
	return r.execSyncResult(res)
}

func (r *runner) execSyncResult(res *result) (string, error) {
	if res.err != nil {
		// Runtime shutdown must remain an interruption, not a catchable error.
		if loopContext(r.cfg.Loop).Err() != nil {
			return "", res.err
		}
		e := r.rt.NewError("Error", res.err.Error())
		var signal any
		if res.signal != "" {
			signal = res.signal
		}
		if err := errors.Join(
			e.Set("code", res.code),
			e.Set("signal", signal),
			e.Set("stdout", string(res.stdout)),
			e.Set("stderr", string(res.stderr)),
		); err != nil {
			return "", err
		}
		return "", r.rt.Throw(e)
	}
	return string(res.stdout), nil
}

// spawnSync runs a program and reports how it went, whatever that was.
func (r *runner) spawnSync(name string, argv quickjs.Value, opts quickjs.Value) (quickjs.Value, error) {
	res := r.start(name, stringsOf(argv), optionString(opts, "cwd"), optionEnv(opts), optionInput(opts))
	return r.resultValue(res, opts)
}

// execFile runs a program and hands back a promise for its output.
func (r *runner) execFile(name string, argv quickjs.Value, opts quickjs.Value, extra ...string) *quickjs.Promise {
	args := append(stringsOf(argv), extra...)
	dir, env, input := optionString(opts, "cwd"), optionEnv(opts), optionInput(opts)
	return r.execWith(opts, func() *result { return r.start(name, args, dir, env, input) })
}

func (r *runner) execShell(command string, opts quickjs.Value) *quickjs.Promise {
	dir, env, input := optionString(opts, "cwd"), optionEnv(opts), optionInput(opts)
	return r.execWith(opts, func() *result { return r.startShell(command, dir, env, input) })
}

func (r *runner) execWith(opts quickjs.Value, start func() *result) *quickjs.Promise {
	p := r.rt.NewPromise()
	text := optionString(opts, "encoding") != "buffer"

	finish := func(res *result) {
		if res.err != nil {
			// The error carries what the program managed to say, which is
			// usually where the reason is.
			e := r.rt.NewError("Error", res.err.Error())
			e.Set("code", res.code)
			if res.signal != "" {
				e.Set("signal", res.signal)
			}
			if text {
				e.Set("stdout", string(res.stdout))
				e.Set("stderr", string(res.stderr))
			} else {
				e.Set("stdout", r.rt.NewBytes(res.stdout))
				e.Set("stderr", r.rt.NewBytes(res.stderr))
			}
			p.Reject(e)
			return
		}
		out := r.rt.NewObject()
		if text {
			out.Set("stdout", string(res.stdout))
			out.Set("stderr", string(res.stderr))
		} else {
			out.Set("stdout", r.rt.NewBytes(res.stdout))
			out.Set("stderr", r.rt.NewBytes(res.stderr))
		}
		p.Resolve(out)
	}

	if r.cfg.Loop == nil {
		finish(start())
		return p
	}
	loop := r.cfg.Loop
	loop.Begin()
	// A program the runtime's closing kills -- its Context ends first -- has
	// exited by the time Close returns.
	done := make(chan struct{})
	unhook := waitOnClose(r.rt, done)
	var removeOnce sync.Once
	var removeScopeCleanup func()
	removeHook := func() {
		removeOnce.Do(unhook)
		if removeScopeCleanup != nil {
			removeScopeCleanup()
		}
	}
	if scope := loop.executionScope(); scope != nil {
		var err error
		removeScopeCleanup, err = scope.AddCleanupHandle(removeHook)
		if err != nil {
			loop.Done()
			close(done)
			removeHook()
			finish(&result{code: -1, err: err})
			return p
		}
	}
	if err := loop.Go(func(context.Context) {
		defer loop.Done()
		res := start()
		close(done)
		loop.postExecution(func() {
			removeHook()
			finish(res)
		})
	}); err != nil {
		loop.Done()
		close(done)
		removeHook()
		finish(&result{code: -1, err: err})
	}
	return p
}

// resultValue is what spawnSync hands back.
func (r *runner) resultValue(res *result, opts quickjs.Value) (quickjs.Value, error) {
	o := r.rt.NewObject()
	text := optionString(opts, "encoding") != "buffer"
	var stdout, stderr any = string(res.stdout), string(res.stderr)
	if !text {
		stdout, stderr = r.rt.NewBytes(res.stdout), r.rt.NewBytes(res.stderr)
	}
	err := errors.Join(
		o.Set("status", res.code),
		o.Set("stdout", stdout),
		o.Set("stderr", stderr),
	)
	if err != nil {
		return quickjs.Value{}, err
	}
	var signal any
	if res.signal != "" {
		signal = res.signal
	}
	if err := o.Set("signal", signal); err != nil {
		return quickjs.Value{}, err
	}
	if res.err != nil {
		if err := o.Set("error", r.rt.NewError("Error", res.err.Error())); err != nil {
			return quickjs.Value{}, err
		}
	}
	return o, nil
}

// cappedBuffer collects output up to a limit and discards the rest, so that a
// program that writes for ever cannot exhaust the host's memory.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	room := c.limit - c.buf.Len()
	if room <= 0 {
		return len(p), nil
	}
	if len(p) > room {
		c.buf.Write(p[:room])
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

func (c *cappedBuffer) Bytes() []byte { return c.buf.Bytes() }

// stringsOf reads an array of strings from script.
func stringsOf(v quickjs.Value) []string {
	if !v.IsArray() {
		return nil
	}
	out := make([]string, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		el, err := v.Index(i)
		if err != nil {
			continue
		}
		out = append(out, el.String())
	}
	return out
}

func optionString(v quickjs.Value, name string) string {
	if v.Kind() != quickjs.KindObject {
		return ""
	}
	got, err := v.Get(name)
	if err != nil || got.Kind() != quickjs.KindString {
		return ""
	}
	return got.String()
}

func optionInput(v quickjs.Value) []byte {
	if v.Kind() != quickjs.KindObject {
		return nil
	}
	got, err := v.Get("input")
	if err != nil || got.IsNullish() {
		return nil
	}
	if b, ok := got.Bytes(); ok {
		return append([]byte(nil), b...)
	}
	return []byte(got.String())
}

// optionEnv reads an environment from script, or reports none.
func optionEnv(v quickjs.Value) map[string]string {
	if v.Kind() != quickjs.KindObject {
		return nil
	}
	got, err := v.Get("env")
	if err != nil || got.Kind() != quickjs.KindObject {
		return nil
	}
	out := map[string]string{}
	for _, k := range got.Keys() {
		val, err := got.Get(k)
		if err != nil {
			continue
		}
		out[k] = val.String()
	}
	return out
}
