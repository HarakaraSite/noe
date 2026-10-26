package stdlib

import (
	"context"
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// Messaging: structuredClone, the ports of a MessageChannel, and
// BroadcastChannel.
//
// A port's two ends are entangled, and each may be anywhere: in the runtime
// that made the channel, in another runtime a port was posted to, or between
// the two, on its way. What an end is, apart from the script's object for it,
// is a portCore, which the goroutines of both ends share; a message posted
// through one end is serialized, queued on the other end's core, and
// deserialized and dispatched on the goroutine of whichever runtime holds that
// end by then, one message to a task.
//
// A worker talks to the runtime that started it through such a channel, and
// its word that it is running, that it failed, and that it has ended travels
// the same way, so that it arrives after what the worker posted before.

// portCore is one end of a channel.
type portCore struct {
	mu sync.Mutex
	// peer is the end this one is entangled with, until either is closed.
	peer *portCore
	// owner is the runtime's end holding this one, and nil while the port is
	// being posted somewhere.
	owner *portEnd
	// queue is what has arrived and not been dispatched.
	queue []portMsg
	// started records that the owner dispatches messages, which a port does
	// once it has been started; scheduled, that a task to dispatch the next
	// is on its way to the owner.
	started, scheduled bool
	closed             bool
}

// msgKind is what a portMsg says.
type msgKind uint8

const (
	// msgData is a message posted.
	msgData msgKind = iota
	// msgClose says the other port end has closed. A Worker may still run.
	msgClose
	// msgWorkerExit follows runtime and initializer cleanup, independently of
	// whether the Worker already closed its parentPort.
	msgWorkerExit
	// msgOnline and msgError are a worker's word that it is running, and
	// that it failed, with the error.
	msgOnline
	msgError
)

// portMsg is a message on its way: a serialized value and the ports posted
// with it, or word from the other end.
type portMsg struct {
	kind  msgKind
	data  any
	ports []*portCore
	code  int
}

// portEnd is a runtime's hold on a port: its id, and the script's object.
type portEnd struct {
	m    *messaging
	id   int
	core *portCore
	obj  quickjs.Value
	// reffed is whether a started port keeps the loop running, which it does
	// until it is unref'd; holding, whether it is doing so.
	reffed, holding bool
	// notify is an optional peer-lifecycle handler supplied by Worker. It runs
	// on this owner alongside data delivery and returns whether it consumed
	// the notification. It may retain the end after the data channel closes.
	notify func(portMsg) bool
	// broadcast owns name registration for an end that is a BroadcastChannel.
	broadcast *broadcastSubscription
	// scopeUnhook removes its execution cleanup after an explicit close.
	scopeUnhook func()
	// work is how what arrives from other goroutines reaches the end's
	// runtime. It does not keep the runtime busy: hold does that for a port
	// that is listening, as node's does.
	work *quickjs.AsyncWork
}

// messaging is a runtime's ports, and the script functions they are made
// and dispatched with.
type messaging struct {
	rt    *quickjs.Runtime
	loop  *Loop
	ports map[int]*portEnd
	next  int
	// halted ends when a worker is terminated, from when what it posts goes
	// nowhere: it would have been stopped by then, were it not between
	// checks for that. It is nil but in a worker.
	halted context.Context

	// What the script provides: a port object for an id, a DOMException, a
	// look at what kind of host object a value is, and the dispatch of a
	// message event; and receiveMessageOnPort, for worker_threads.
	makePort, newDOMException, hostKind, deliver, receive, makeBlob quickjs.Value
}

// messagingHost is the functions webAPIsJS reaches messaging through.
func (m *messaging) host() map[string]any {
	return map[string]any{
		"bindMessaging": func(makePort, newDOMException, hostKind, deliver, receive, makeBlob quickjs.Value) {
			m.makePort, m.newDOMException, m.hostKind, m.deliver = makePort, newDOMException, hostKind, deliver
			m.receive, m.makeBlob = receive, makeBlob
		},
		"clone": m.clone,
		"channel": func() (quickjs.Value, error) {
			a, b := &portCore{}, &portCore{}
			a.peer, b.peer = b, a
			pa, err := m.adopt(a)
			if err != nil {
				return quickjs.Value{}, err
			}
			pb, err := m.adopt(b)
			if err != nil {
				return quickjs.Value{}, err
			}
			return m.rt.NewArray(pa, pb)
		},
		"portPost": func(id int, v quickjs.Value, transfer []quickjs.Value) error {
			if m.isHalted() {
				return nil
			}
			e := m.ports[id]
			if e == nil {
				// A port posted elsewhere, or closed, posts nothing -- but the
				// message is serialized all the same, as the standard has it:
				// what cannot be cloned throws, and what is transferred is
				// detached, and closed if it is a port.
				data, ports, err := m.serialize(v, transfer, nil)
				if err != nil {
					return err
				}
				dropMessages(portMsg{data: data, ports: ports})
				return nil
			}
			data, ports, err := m.serialize(v, transfer, e)
			if err != nil {
				return err
			}
			e.core.mu.Lock()
			peer := e.core.peer
			e.core.mu.Unlock()
			msg := portMsg{data: data, ports: ports}
			if peer == nil {
				// Posted to a port whose other end has closed, the message
				// goes nowhere, and the ports it carries are closed.
				dropMessages(msg)
				return nil
			}
			peer.enqueue(msg)
			return nil
		},
		"portStart": func(id int) {
			if e := m.ports[id]; e != nil {
				e.start()
			}
		},
		"portClose": func(id int) {
			if e := m.ports[id]; e != nil {
				e.close()
			}
		},
		"portRef": func(id int, ref bool) {
			if e := m.ports[id]; e != nil {
				e.reffed = ref
				e.hold()
			}
		},
		"portHasRef": func(id int) bool {
			e := m.ports[id]
			return e != nil && e.holding
		},
		// portReceive takes the next message waiting on a port, as
		// receiveMessageOnPort does, in a list; or null for none.
		"portReceive": func(id int) (any, error) {
			e := m.ports[id]
			if e == nil {
				return nil, nil
			}
			c := e.core
			c.mu.Lock()
			if len(c.queue) == 0 || c.queue[0].kind != msgData {
				c.mu.Unlock()
				return nil, nil
			}
			msg := c.queue[0]
			c.queue = c.queue[1:]
			c.mu.Unlock()
			v, _, err := m.deserialize(msg.data, msg.ports)
			if err != nil {
				return nil, err
			}
			return m.rt.NewArray(v)
		},
		"broadcastOpen": m.openBroadcast,
		"broadcastPost": m.postBroadcast,
	}
}

// isHalted is whether the worker these are the ports of has been
// terminated.
func (m *messaging) isHalted() bool {
	return m.halted != nil && m.halted.Err() != nil
}

// adopt makes core an end of this runtime's, and returns its object.
func (m *messaging) adopt(core *portCore) (quickjs.Value, error) {
	e, err := m.adoptEnd(core)
	if err != nil {
		return quickjs.Value{}, err
	}
	return e.obj, nil
}

// adoptEnd is adopt, returning the end.
func (m *messaging) adoptEnd(core *portCore) (*portEnd, error) {
	m.next++
	e := &portEnd{m: m, id: m.next, core: core, reffed: true, work: m.startWork()}
	obj, err := m.makePort.Call(e.id)
	if err != nil {
		return nil, err
	}
	e.obj = obj
	m.ports[e.id] = e
	core.mu.Lock()
	core.owner, core.started, core.scheduled = e, false, false
	core.mu.Unlock()
	if err := e.watchExecution(); err != nil {
		e.closeForExecution()
		return nil, err
	}
	return e, nil
}

// --- Ends ----------------------------------------------------------------------

// enqueue queues a message on a port, and has its owner told if it is
// listening -- or, for a close, whether it is listening or not.
func (c *portCore) enqueue(msg portMsg) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		dropMessages(msg)
		return
	}
	c.queue = append(c.queue, msg)
	owner := c.owner
	post := owner != nil && (c.started || msg.kind == msgClose) && !c.scheduled
	if post {
		c.scheduled = true
	}
	c.mu.Unlock()
	if post {
		owner.schedule()
	}
}

// schedule has the end's runtime dispatch the next message, from any
// goroutine. Once the end is no longer the runtime's, or the runtime has
// closed, there is nothing to dispatch to.
func (e *portEnd) schedule() {
	e.work.Post(func(err error) {
		if err == nil && !e.executionClosing() {
			e.dispatchNext()
		}
	})
}

// watchExecution releases a port with the scope that owns its runtime. A
// transferred port removes this cleanup before it changes owners.
func (e *portEnd) watchExecution() error {
	if e.m == nil || e.m.loop == nil {
		return nil
	}
	if scope := e.m.loop.executionScope(); scope != nil {
		var err error
		e.scopeUnhook, err = scope.AddCleanupHandle(e.closeForExecution)
		return err
	}
	return nil
}

func (e *portEnd) executionClosing() bool {
	if e == nil || e.m == nil || e.m.loop == nil {
		return false
	}
	if scope := e.m.loop.executionScope(); scope != nil {
		return scope.isClosing()
	}
	return false
}

// closeForExecution releases the port without delivering a JavaScript close
// event. The scope is ending, but the borrowed runtime remains usable.
func (e *portEnd) closeForExecution() {
	if e == nil {
		return
	}
	if e.broadcast != nil {
		e.closeBroadcast()
		return
	}
	if e.m.halted != nil {
		// A worker's shutdown immediately follows its scope cleanup and keeps
		// parentPort until it can send the worker's actual exit code.
		if e.scopeUnhook != nil {
			e.scopeUnhook()
			e.scopeUnhook = nil
		}
		return
	}
	c := e.core
	c.mu.Lock()
	if c.owner == e {
		c.owner, c.started, c.scheduled = nil, false, false
	}
	c.mu.Unlock()
	e.release()
	c.disentangle(0)
}

// startWork is what one of the runtime's ends hears through: work that does
// not of itself keep the runtime busy.
func (m *messaging) startWork() *quickjs.AsyncWork {
	w := m.rt.StartAsyncWork()
	w.Unref()
	return w
}

// dispatchNext dispatches the message at the head of the queue, on the
// owner's goroutine, and asks for another task for the next.
func (e *portEnd) dispatchNext() {
	c := e.core
	c.mu.Lock()
	if c.owner != e {
		// The port was posted elsewhere after the task was asked for; its new
		// owner has asked for its own.
		c.mu.Unlock()
		return
	}
	var msg portMsg
	var dropped []portMsg
	switch {
	case len(c.queue) == 0 || c.closed:
		c.scheduled = false
		c.mu.Unlock()
		return
	case c.started:
		msg, c.queue = c.queue[0], c.queue[1:]
	default:
		// A port that was never started hears only that it has closed, and
		// what was sent it before is dropped.
		at := -1
		for i, q := range c.queue {
			if q.kind == msgClose {
				at = i
				break
			}
		}
		if at < 0 {
			c.scheduled = false
			c.mu.Unlock()
			return
		}
		dropped = c.queue[:at]
		msg, c.queue = c.queue[at], nil
	}
	c.mu.Unlock()
	dropMessages(dropped...)

	if e.notify == nil || !e.notify(msg) {
		if msg.kind == msgClose {
			e.closed()
		} else {
			e.dispatch(msg)
		}
	}

	c.mu.Lock()
	again := c.owner == e && !c.closed && len(c.queue) > 0 && c.started
	if c.owner == e && !again {
		c.scheduled = false
	}
	c.mu.Unlock()
	if again {
		e.schedule()
	}
}

// dispatch deserializes a message and dispatches it to the port's listeners;
// one that cannot be deserialized is a messageerror.
func (e *portEnd) dispatch(msg portMsg) {
	m := e.m
	v, ports, err := m.deserialize(msg.data, msg.ports)
	if err != nil {
		m.raise(m.deliver.Call(e.obj, "messageerror"))
		return
	}
	items := make([]any, len(ports))
	for i, p := range ports {
		items[i] = p
	}
	list, err := m.rt.NewArray(items...)
	if err != nil {
		return
	}
	m.raise(m.deliver.Call(e.obj, "message", v, list))
}

// raise makes an exception a listener threw uncaught, as one a timer throws
// is: Run returns it. Without a loop there is no one to tell.
func (m *messaging) raise(_ quickjs.Value, err error) {
	if err != nil && m.loop != nil {
		m.loop.fail(err)
	}
}

// start begins dispatching what arrives.
func (e *portEnd) start() {
	c := e.core
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return
	}
	c.started = true
	post := len(c.queue) > 0 && !c.scheduled
	if post {
		c.scheduled = true
	}
	c.mu.Unlock()
	e.hold()
	if post {
		e.schedule()
	}
}

// close disentangles the port. Both ends hear it, as node's do, this one
// first and the other after what this one sent before. A BroadcastChannel
// stops hearing its name.
func (e *portEnd) close() {
	if e.broadcast != nil {
		e.closeBroadcast()
		return
	}
	e.release()
	// The close event is a task of its own, which the program waits for.
	e.m.rt.StartAsyncWork().Complete(func(err error) {
		if err == nil && !e.executionClosing() {
			e.m.raise(e.m.deliver.Call(e.obj, "close"))
		}
	})
	e.core.disentangle(0)
}

// disentangle closes a port's core, and tells the other end, after what this
// one sent before: with the exit code, when this is a worker's end.
func (c *portCore) disentangle(code int) {
	c.mu.Lock()
	peer, dropped := c.peer, c.queue
	c.peer, c.closed, c.queue = nil, true, nil
	c.mu.Unlock()
	dropMessages(dropped...)
	if peer != nil {
		peer.mu.Lock()
		peer.peer = nil
		peer.mu.Unlock()
		peer.enqueue(portMsg{kind: msgClose, code: code})
	}
}

// finish closes the core, drops queued transfers and releases this end. It is
// Go-only: the caller decides whether peer close or Worker exit is published.
func (e *portEnd) finish() {
	c := e.core
	c.mu.Lock()
	dropped := c.queue
	c.closed, c.queue = true, nil
	c.mu.Unlock()
	dropMessages(dropped...)
	e.release()
}

// closed releases an ordinary port on peer close and notifies its listeners.
func (e *portEnd) closed() {
	e.finish()
	e.m.raise(e.m.deliver.Call(e.obj, "close"))
}

// shutdown closes every port of a runtime that is ending but keep, as a
// worker's are when it has ended: the other ends hear it, and nothing is held
// open for a runtime that is gone.
func (m *messaging) shutdown(keep *portCore) {
	for _, e := range m.ports {
		switch {
		case e.core == keep:
		case e.broadcast != nil:
			e.closeBroadcast()
		default:
			e.release()
			e.core.disentangle(0)
		}
	}
}

// dropMessages closes the ports inside messages that will never be
// delivered, as node does: their other ends hear that they have closed,
// rather than holding their loops open waiting on ports no runtime has.
func dropMessages(msgs ...portMsg) {
	for _, msg := range msgs {
		for _, c := range msg.ports {
			c.disentangle(0)
		}
	}
}

// detach lets go of a port being posted: its messages wait in its queue for
// whichever runtime it arrives in.
func (e *portEnd) detach() {
	c := e.core
	c.mu.Lock()
	c.owner, c.started, c.scheduled = nil, false, false
	c.mu.Unlock()
	e.release()
}

// release is the end being this runtime's no longer.
func (e *portEnd) release() {
	if e.scopeUnhook != nil {
		e.scopeUnhook()
		e.scopeUnhook = nil
	}
	e.work.Done()
	delete(e.m.ports, e.id)
	e.reffed = false
	e.hold()
}

// hold keeps the loop running while the port is started and ref'd, as a
// listening port keeps node's.
func (e *portEnd) hold() {
	e.core.mu.Lock()
	want := e.reffed && e.core.started && e.core.owner == e && !e.core.closed
	e.core.mu.Unlock()
	if want == e.holding || e.m.loop == nil {
		return
	}
	e.holding = want
	if want {
		e.m.loop.Begin()
	} else {
		e.m.loop.Done()
	}
}
