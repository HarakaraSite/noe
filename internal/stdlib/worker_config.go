package stdlib

import (
	"errors"
	"io"
	"maps"
	"slices"
	"sync"
)

// InheritWorkerState carries the parts of a worker Config that identify and
// account for its execution when an explicit Profile installer rebuilds the
// capability selection. Profile-owned process metadata may change, but worker
// lifecycle callbacks and the shared worker count must survive.
func InheritWorkerState(dst *Config, src Config) {
	if dst == nil {
		return
	}
	dst.worker = src.worker
	dst.running = src.running
	if dst.Process == nil || src.Process == nil {
		return
	}
	dst.Process.worker = src.Process.worker
	dst.Process.Exit = src.Process.Exit
	dst.Process.Unhandled = src.Process.Unhandled
	dst.Process.interruptExit = src.Process.interruptExit
}

// forWorker derives a child's settings from its parent before the child has a
// runtime. Process metadata, args and env become child-owned here; capabilities,
// their backing resources, output locks and the worker count retain the parent's
// sharing. bindWorkerLoop completes the runtime-bound copies on the child owner.
func (c Config) forWorker(w *workerContext, specifier string, o workerOptions) Config {
	c.worker = w
	c.Loop = nil
	script := specifier
	if o.Eval {
		script = "[worker eval]"
	}
	exe := "qjs"
	if len(c.environmentArgs) > 0 {
		exe = c.environmentArgs[0]
	} else if c.Process != nil && len(c.Process.Args) > 0 {
		exe = c.Process.Args[0]
	}
	c.environmentArgs = append([]string{exe, script}, o.Argv...)
	if o.HasEnv {
		c.environmentEnv = maps.Clone(o.Env)
	} else if c.environmentEnv != nil {
		c.environmentEnv = maps.Clone(c.environmentEnv)
	} else if c.Process != nil {
		// Legacy Config has no execution environment snapshot. Inherit the
		// process environment unless the worker options explicitly override it.
		c.environmentEnv = maps.Clone(c.Process.Env)
	} else {
		c.environmentEnv = nil
	}
	if c.Process != nil {
		p := *c.Process
		p.Stdin = nil
		p.Exit, p.worker = w.exit, true
		// The parent's handling of its own rejections is not the worker's.
		p.Unhandled = nil
		p.Args = append([]string(nil), c.environmentArgs...)
		p.Env = maps.Clone(c.environmentEnv)
		c.Process = &p
	}
	return c
}

// bindWorkerLoop completes a forWorker Config on the child's runtime owner.
// Copy only the loop-bound capability settings, not their backing resources
// (notably FS.Backend). DNS's mutable Servers slice is child-owned; Sockets must
// refer to the child's Serve, whose runtime-specific upgrade callback is reset.
// Keep this copy/rebind policy here rather than in Worker startup.
func (c *Config) bindWorkerLoop(loop *Loop) {
	c.Loop = loop
	if c.FS != nil {
		fs := *c.FS
		fs.Loop = loop
		c.FS = &fs
	}
	if c.Fetch != nil {
		f := *c.Fetch
		f.Loop = loop
		c.Fetch = &f
	}
	if c.DNS != nil {
		d := *c.DNS
		d.Loop = loop
		d.Servers = slices.Clone(c.DNS.Servers)
		c.DNS = &d
	}
	if c.Run != nil {
		r := *c.Run
		r.Loop = loop
		c.Run = &r
	}
	if c.Serve != nil {
		s := *c.Serve
		s.Loop, s.upgrade = loop, nil
		c.Serve = &s
	}
	if c.Sockets != nil {
		ws := *c.Sockets
		ws.Loop = loop
		if ws.Serve != nil {
			ws.Serve = c.Serve
		}
		c.Sockets = &ws
	}
}

// syncWriter is a writer shared by a program and its workers, which write to
// it from goroutines of their own: one write at a time.
type syncWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (s syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// shareWriters makes the writers of a Config that starts workers safe for
// them to share: each writer, however many fields name it, gets one lock.
func (c *Config) shareWriters() {
	locks := map[io.Writer]*sync.Mutex{}
	wrap := func(w io.Writer) io.Writer {
		if w == nil {
			return nil
		}
		if _, ok := w.(syncWriter); ok {
			return w
		}
		mu := locks[w]
		if mu == nil {
			mu = &sync.Mutex{}
			locks[w] = mu
		}
		return syncWriter{mu: mu, w: w}
	}
	c.Stdout, c.Stderr = wrap(c.Stdout), wrap(c.Stderr)
	if c.Process != nil {
		p := *c.Process
		p.Stdout, p.Stderr = wrap(p.Stdout), wrap(p.Stderr)
		c.Process = &p
	}
}

// errNoLoop is Workers without a Loop to run the workers' messages on.
var errNoLoop = errors.New("stdlib: Workers needs a Loop")
