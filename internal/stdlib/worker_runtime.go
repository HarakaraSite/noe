package stdlib

import (
	"context"
	"errors"
	"strings"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// uncaughtRejection is a promise rejected with nothing to handle it, which
// ends a worker as an uncaught exception does.
type uncaughtRejection struct{ reason quickjs.Value }

func (u *uncaughtRejection) Error() string { return "uncaught (in promise) " + u.reason.String() }

// runWorker owns the child's VM, installation, entry evaluation and cleanup.
// Failures are converted to Go data before closing the VM. The caller keeps
// worker accounting and done/exit publication separate: returning here means
// cleanup and runtime close have finished, not merely that its port closed.
func runWorker(ctx context.Context, cfg Config, w *workerContext, wh *workerHandle, specifier string, o workerOptions) (code int) {
	var fail error
	var rt *quickjs.Runtime
	var installation WorkerInstallation
	defer func() {
		if c, ok := wh.exitCode(); ok {
			// Ended by process.exit or terminate, whose code it is, and
			// whatever was running when it was stopped is no failure.
			code, fail = c, nil
		} else if fail == nil && rt != nil {
			// Ended of itself, with the code it set, if it set one.
			if c, ok := ExitCode(rt); ok {
				code = c
			}
		}
		if fail != nil {
			code = 1
			w.parent.enqueue(portMsg{kind: msgError, data: w.errorData(rt, fail)})
		}
		if cfg.Loop != nil {
			if installation.Close != nil {
				installation.Close()
			}
			if scope := cfg.Loop.executionScope(); scope != nil {
				scope.Close()
			}
		}
		if w.m != nil {
			w.m.shutdown(w.port)
		}
		if cfg.Loop != nil {
			cfg.Loop.Close()
		}
		if rt != nil {
			rt.Close()
		}
		w.port.disentangle(code)
	}()
	if ctx.Err() != nil {
		return
	}

	var err error
	if cfg.Workers != nil && cfg.Workers.New != nil {
		rt, err = cfg.Workers.New()
	} else {
		rt = quickjs.New()
	}
	if err != nil {
		fail = err
		return
	}
	if rt == nil {
		fail = errors.New("the host made no runtime for the worker")
		return
	}
	if ctx.Err() != nil {
		return
	}
	// The loop's context ends when the worker is terminated, and with it
	// what the worker's host functions are waiting on: a program it is
	// running, a request.
	loop := newLoop(rt, ctx)
	cfg.bindWorkerLoop(loop)
	w.close = func() {
		// What is running runs to its end, and then the loop, closed,
		// runs nothing more.
		wh.setCode(0)
		loop.Close()
	}
	rt.AbortOn(ctx.Done())
	// A rejection nothing handles ends the worker, as in node -- unless the
	// worker listens for process's "unhandledRejection", which handles it.
	// The process is told before it is installed, when it takes it.
	failWith := func(reason quickjs.Value) { loop.fail(&uncaughtRejection{reason}) }
	if cfg.Process != nil {
		cfg.Process.Unhandled = failWith
	}
	if cfg.workerInstaller != nil {
		installation, err = cfg.workerInstaller(rt, loop, cfg)
	} else {
		err = Install(rt, cfg)
	}
	if err != nil {
		fail = err
		return
	}
	if cfg.Process == nil {
		rt.OnUnhandledRejection(failWith)
	}
	if cfg.Workers != nil && cfg.Workers.Installed != nil {
		if err := cfg.Workers.Installed(rt); err != nil {
			fail = err
			return
		}
	}

	src, name, module := specifier, "[worker eval]", false
	if cfg.workerInstaller != nil {
		if installation.Prepare == nil {
			fail = errors.New("this runtime cannot prepare a worker's code")
			return
		}
		src, name, module, err = installation.Prepare(specifier, o.Eval, o.Type)
		if err != nil {
			fail = err
			return
		}
	} else {
		switch {
		case o.Eval:
			// Code that parses only as a module is one.
			module = o.Type == "module" || rt.CheckSyntax(src) != nil && rt.CheckModuleSyntax(src) == nil
		case cfg.Workers == nil || cfg.Workers.Load == nil:
			fail = errors.New("this runtime cannot load a worker's code, only run code passed with eval")
			return
		default:
			if src, name, module, err = cfg.Workers.Load(specifier); err != nil {
				fail = err
				return
			}
			switch o.Type {
			case "module":
				module = true
			case "classic":
				module = false
			}
		}
	}

	// A web worker reports an exception to its parent and runs on, as the
	// HTML standard has it; a node worker ends with it.
	report := func(err error) bool {
		var jsErr *quickjs.Error
		var rejection *uncaughtRejection
		if !w.web || !errors.As(err, &jsErr) && !errors.As(err, &rejection) {
			return false
		}
		w.parent.enqueue(portMsg{kind: msgError, data: w.errorData(rt, err)})
		return true
	}
	loop.report = report

	w.parent.enqueue(portMsg{kind: msgOnline})
	if module {
		_, err = rt.EvalModuleContext(ctx, name, src)
	} else {
		_, err = rt.EvalFileContext(ctx, name, src)
	}
	if err == nil || report(err) {
		err = loop.Run(ctx)
	}
	fail = err
	return
}

// errorData is what a worker failed with, serialized for its parent: the
// exception a script threw, cloned, or failing that the error as a message.
func (w *workerContext) errorData(rt *quickjs.Runtime, err error) any {
	var v quickjs.Value
	var jsErr *quickjs.Error
	var rejection *uncaughtRejection
	var syntax *quickjs.SyntaxError
	switch {
	case w.m == nil || rt == nil:
		return err
	case errors.As(err, &jsErr):
		v = jsErr.Value()
	case errors.As(err, &rejection):
		v = rejection.reason
	case errors.As(err, &syntax):
		// Code that did not compile is a SyntaxError, as the parent hears it.
		v = rt.NewError("SyntaxError", strings.TrimPrefix(syntax.Error(), "quickjs: SyntaxError: "))
	default:
		return err
	}
	data, _, serr := w.m.serialize(v, nil, nil)
	if serr != nil {

		return errors.New(v.String())
	}
	return data
}
