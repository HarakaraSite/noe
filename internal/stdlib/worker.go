package stdlib

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// Workers lets a script start workers -- node:worker_threads' Worker, and the
// web's -- each a runtime of its own, running on a goroutine of its own with a
// Loop of its own, which the script talks to by posting messages.
//
// A worker is installed with the same Config as the runtime that starts it:
// it can reach what its parent can, and nothing else. Its process.exit ends
// the worker rather than the program, and it has no stdin.
type Workers struct {
	// New makes a worker's runtime, as the host makes its own: with the
	// same options, and the same module loader. Nil makes one with
	// quickjs.New, which can import nothing but the modules installed here.
	New func() (*quickjs.Runtime, error)

	// Installed is called with a worker's runtime once everything the
	// Config asks for is installed in it, for the host to add what it adds to
	// its own. Nil adds nothing.
	Installed func(rt *quickjs.Runtime) error

	// Load reads a worker's code: the file or URL a script named, as it
	// named it. It returns the source, the name to evaluate it under -- the
	// resolved path, which the module loader resolves the worker's own
	// imports against -- and whether it is a module. Nil loads nothing, and
	// only code passed with eval can start a worker.
	Load func(specifier string) (source, name string, module bool, err error)

	// Max is the most workers that may run at once, every worker a worker
	// starts counted with the rest: past it, new Worker throws. Zero means
	// 64; a negative value, no limit. Without one a script could start
	// workers -- each a runtime and a goroutine, which no memory limit
	// counts -- until the process ran out.
	Max int
}

// defaultMaxWorkers is what a zero Workers.Max means.
const defaultMaxWorkers = 64

// takeWorker counts one more worker running, reporting false, and counting
// none, where Max are running already.
func (c *Config) takeWorker() bool {
	max := c.Workers.Max
	if max == 0 {
		max = defaultMaxWorkers
	}
	if max < 0 || c.running == nil {
		return true
	}
	for {
		n := c.running.Load()
		if n >= int64(max) {
			return false
		}
		if c.running.CompareAndSwap(n, n+1) {
			return true
		}
	}
}

// releaseWorker counts a worker takeWorker counted as no longer running.
func (c *Config) releaseWorker() {
	if c.running != nil && c.Workers.Max >= 0 {
		c.running.Add(-1)
	}
}

// threadIDs numbers the workers the process starts, as node's threadId does;
// the program itself is thread 0.
var threadIDs atomic.Int64

// workerContext is what a worker's runtime knows of itself.
type workerContext struct {
	threadID int
	name     string
	// web marks a worker the web's Worker started, whose global scope is a
	// worker's: self, postMessage, onmessage and close.
	web bool
	// port is the worker's end of the channel to its parent.
	port *portCore
	// parent receives lifecycle events even after parentPort is disentangled.
	// It shares the data queue so error and exit stay after earlier messages.
	parent *portCore
	// data is workerData and the environment data, serialized, and the
	// ports posted with them.
	data  any
	ports []*portCore
	// m is the worker's messaging, once it is installed.
	m *messaging
	// exit ends the worker at once, with a code: process.exit. close ends
	// it once what is running returns, as a web worker's close does.
	exit  func(code int)
	close func()
	// ctx ends when the worker is terminated.
	ctx context.Context
}

// workerHost is a runtime's workers: those it started, and what it needs to
// start more.
type workerHost struct {
	rt   *quickjs.Runtime
	cfg  Config
	m    *messaging
	loop *Loop
	// self is the runtime's own place, if it is a worker.
	self *workerContext
	// event is the script's function that tells a Worker object what its
	// worker said.
	event quickjs.Value

	mu      sync.Mutex
	workers map[*workerHandle]bool
}

// workerHandle is a worker, as the runtime that started it holds it.
type workerHandle struct {
	host *workerHost
	obj  quickjs.Value
	// reffed is whether the worker keeps its parent's loop running, which it
	// does until it is unref'd or ends; holding, whether it is doing so.
	reffed, holding, ended bool
	// stop ends the worker: terminate, or the parent's loop closing.
	stop context.CancelFunc
	// done is closed when the worker's goroutine has returned, its runtime
	// closed.
	done chan struct{}

	// code is the exit code the worker was ended with, by process.exit or by
	// being terminated, before it ended of itself.
	mu     sync.Mutex
	code   int
	coded  bool
	thread int
}

// installWorkers installs node:worker_threads -- whose Worker starts one only
// if the Config has Workers -- and, where it has, the web's Worker; and in a
// worker's runtime, what the worker's code finds: its parentPort, its
// workerData, and a web worker's global scope.
func installWorkers(rt *quickjs.Runtime, cfg Config, m *messaging, events quickjs.Value) error {
	h := &workerHost{rt: rt, cfg: cfg, m: m, loop: cfg.Loop, self: cfg.worker, workers: map[*workerHandle]bool{}}
	if cfg.Workers != nil && cfg.Loop != nil {
		// A runtime whose loop is closed has no use for its workers, and
		// one that is closed has them ended before its Close returns.
		context.AfterFunc(loopContext(cfg.Loop), h.terminateAll)
		if scope := cfg.Loop.executionScope(); scope != nil {
			if err := scope.AddCleanup(h.endAll); err != nil {
				return err
			}
		}
		rt.OnClose(h.endAll)
	}
	host := rt.NewObject()
	if err := setAll(host, map[string]any{
		"EventEmitter":         events,
		"receiveMessageOnPort": m.receive,
		"bindWorkers": func(event quickjs.Value) {
			h.event = event
		},
		"info":      h.info,
		"start":     h.start,
		"terminate": func(w quickjs.Value) { h.handle(w, func(wh *workerHandle) { wh.terminate(1) }) },
		"ref": func(w quickjs.Value, ref bool) {
			h.handle(w, func(wh *workerHandle) {
				wh.reffed = ref
				wh.hold()
			})
		},
		"exit": func(code int) {
			if h.self != nil {
				h.self.exit(code)
			}
		},
		"close": func() {
			if h.self != nil && h.self.close != nil {
				h.self.close()
			}
		},
	}); err != nil {
		return err
	}
	api, err := evalWithHost(rt, "<worker_threads>", workerJS, host)
	if err != nil {
		return err
	}
	threads, err := api.Get("worker_threads")
	if err != nil {
		return err
	}
	exports, err := moduleExports(threads)
	if err != nil {
		return err
	}
	if err := rt.SetModuleValues("worker_threads", exports); err != nil {
		return err
	}
	if err := rt.SetModuleValues("node:worker_threads", exports); err != nil {
		return err
	}
	globals := []string{"ErrorEvent"}
	if cfg.Workers != nil && cfg.Loop != nil {
		globals = append(globals, "Worker")
	}
	for _, name := range globals {
		v, err := api.Get(name)
		if err != nil {
			return err
		}
		if err := rt.Set(name, v); err != nil {
			return err
		}
	}
	return nil
}

// info is what the script needs to know of where it runs: whether it is a
// worker, and if so the port to its parent and the data it was given.
func (h *workerHost) info() (quickjs.Value, error) {
	o := h.rt.NewObject()
	w := h.self
	if w == nil {
		return o, setAll(o, map[string]any{"isMainThread": true, "threadId": 0})
	}
	port, err := h.m.adopt(w.port)
	if err != nil {
		return quickjs.Value{}, err
	}
	data, _, err := h.m.deserialize(w.data, w.ports)
	if err != nil {
		return quickjs.Value{}, err
	}
	return o, setAll(o, map[string]any{
		"isMainThread": false, "threadId": w.threadID, "parentPort": port,
		"data": data, "web": w.web, "name": w.name,
	})
}

// handle finds the worker a Worker object stands for.
func (h *workerHost) handle(obj quickjs.Value, fn func(*workerHandle)) {
	h.mu.Lock()
	var found *workerHandle
	for wh := range h.workers {
		if wh.obj.StrictEqual(obj) {
			found = wh
			break
		}
	}
	h.mu.Unlock()
	if found != nil {
		fn(found)
	}
}

// workerOptions is what the script passes to start a worker.
type workerOptions struct {
	Eval   bool              `js:"eval"`
	Argv   []string          `js:"argv"`
	Env    map[string]string `js:"env"`
	HasEnv bool              `js:"hasEnv"`
	Name   string            `js:"name"`
	Web    bool              `js:"web"`
	// Type is a web worker's: "module" or "classic"; or "" for node's,
	// whose code says which it is.
	Type string `js:"type"`
}

// start starts a worker for obj, the script's Worker: the code a specifier
// names, or the code itself; given data -- workerData and the environment
// data -- with transfer transferred. It returns the port to the worker and
// the worker's thread id.
func (h *workerHost) start(obj quickjs.Value, specifier string, data quickjs.Value,
	transfer []quickjs.Value, opts quickjs.Value) (quickjs.Value, error) {
	if h.cfg.Workers == nil || h.loop == nil {
		return quickjs.Value{}, h.rt.Throw(h.rt.NewError("Error", "this runtime may not start workers"))
	}
	if !h.cfg.takeWorker() {
		max := h.cfg.Workers.Max
		if max == 0 {
			max = defaultMaxWorkers
		}
		return quickjs.Value{}, h.rt.Throw(h.rt.NewError("Error",
			fmt.Sprintf("too many workers: at most %d may run at once", max)))
	}
	// The worker is counted until its goroutine ends, or, if it never
	// starts, until this returns.
	started := false
	defer func() {
		if !started {
			h.cfg.releaseWorker()
		}
	}()
	var o workerOptions
	if err := opts.Decode(&o); err != nil {
		return quickjs.Value{}, err
	}
	serialized, ports, err := h.m.serialize(data, transfer, nil)
	if err != nil {
		return quickjs.Value{}, err
	}
	parent, child := &portCore{}, &portCore{}
	parent.peer, child.peer = child, parent
	end, err := h.m.adoptEnd(parent)
	if err != nil {
		return quickjs.Value{}, err
	}

	ctx, stop := context.WithCancel(loopContext(h.loop))
	wh := &workerHandle{host: h, obj: obj, reffed: true, stop: stop, thread: int(threadIDs.Add(1)), done: make(chan struct{})}
	end.notify = wh.portNotifications(end.finish)
	// The port to a worker dispatches at once; the worker, not the port,
	// is what holds the loop.
	end.reffed = false
	end.start()
	h.mu.Lock()
	h.workers[wh] = true
	h.mu.Unlock()
	wh.hold()

	w := &workerContext{threadID: wh.thread, name: o.Name, web: o.Web, port: child, parent: parent, data: serialized, ports: ports, ctx: ctx}
	w.exit = func(code int) { wh.terminate(code) }
	cfg := h.cfg.forWorker(w, specifier, o)
	started = true
	go func() {
		defer close(wh.done)
		defer h.cfg.releaseWorker()
		code := runWorker(ctx, cfg, w, wh, specifier, o)
		parent.enqueue(portMsg{kind: msgWorkerExit, code: code})
	}()

	result := h.rt.NewObject()
	return result, setAll(result, map[string]any{"port": end.obj, "threadId": wh.thread})
}

// hold keeps the parent's loop running while the worker runs and is ref'd.
func (wh *workerHandle) hold() {
	want := wh.reffed && !wh.ended
	if want == wh.holding {
		return
	}
	wh.holding = want
	if want {
		wh.host.loop.Begin()
	} else {
		wh.host.loop.Done()
	}
}

// terminate ends the worker with code, unless it has already ended or been
// given one. It is safe to call from any goroutine.
func (wh *workerHandle) terminate(code int) {
	wh.setCode(code)
	wh.stop()
}

// setCode gives the worker the code it ends with, unless it has one.
func (wh *workerHandle) setCode(code int) {
	wh.mu.Lock()
	if !wh.coded {
		wh.code, wh.coded = code, true
	}
	wh.mu.Unlock()
}

// exitCode is the code the worker was ended with, if it was.
func (wh *workerHandle) exitCode() (int, bool) {
	wh.mu.Lock()
	defer wh.mu.Unlock()
	return wh.code, wh.coded
}

// terminateAll ends every worker the runtime started.
func (h *workerHost) terminateAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for wh := range h.workers {
		wh.terminate(1)
	}
}

// endAll ends every worker the runtime started and joins them. An execution
// scope cannot leave owned workers behind, while legacy runtime close retains
// its historical closeWait bound. The lock is not held while it waits: a
// worker ending takes it.
func (h *workerHost) endAll() {
	h.mu.Lock()
	ending := make([]*workerHandle, 0, len(h.workers))
	for wh := range h.workers {
		wh.terminate(1)
		ending = append(ending, wh)
	}
	h.mu.Unlock()
	if h.loop != nil && h.loop.executionScope() != nil {
		for _, wh := range ending {
			<-wh.done
			// Scope shutdown cannot wait for the parent's queued exit task.
			// Release the loop hold here and discard the ended handle directly.
			wh.ended = true
			wh.hold()
			h.mu.Lock()
			delete(h.workers, wh)
			h.mu.Unlock()
		}
		return
	}
	deadline := time.NewTimer(closeWait)
	defer deadline.Stop()
	for _, wh := range ending {
		select {
		case <-wh.done:
		case <-deadline.C:
			return
		}
	}
}

// portNotifications interprets lifecycle messages on the hidden parent end.
// Messaging owns their FIFO with data and invokes this only on the parent owner.
// A port close is not Worker completion; only exit joins final accounting,
// releases the hidden end and exposes the Worker's exit to JavaScript.
func (wh *workerHandle) portNotifications(finish func()) func(portMsg) bool {
	return func(msg portMsg) bool {
		switch msg.kind {
		case msgClose:
			// Keep the hidden parent end for lifecycle events after parentPort.close.
		case msgWorkerExit:
			// Cleanup finished before enqueue. Join the remaining accounting before
			// releasing the port or removing the handle from the owner's map.
			<-wh.done
			finish()
			wh.exited(msg.code)
		case msgOnline:
			wh.online()
		case msgError:
			wh.failed(msg.data)
		default:
			return false
		}
		return true
	}
}

// online, failed and exited are what the worker said, heard in its parent.

func (wh *workerHandle) online() {
	wh.host.raise(wh.host.event.Call(wh.obj, "online"))
}

func (wh *workerHandle) failed(data any) {
	h := wh.host
	var v quickjs.Value
	if err, ok := data.(error); ok {
		v = h.rt.NewError("Error", err.Error())
	} else {
		var err error
		if v, _, err = h.m.deserialize(data, nil); err != nil {
			v = h.rt.NewError("Error", "the worker failed, with an error that could not be cloned")
		}
	}
	h.raise(h.event.Call(wh.obj, "error", v))
}

func (wh *workerHandle) exited(code int) {
	h := wh.host
	wh.ended = true
	wh.hold()
	h.mu.Lock()
	delete(h.workers, wh)
	h.mu.Unlock()
	h.raise(h.event.Call(wh.obj, "exit", code))
}

// raise makes an exception a Worker's listener threw uncaught.
func (h *workerHost) raise(_ quickjs.Value, err error) {
	if err != nil && h.loop != nil {
		h.loop.fail(err)
	}
}
