package stdlib

import (
	"context"
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// broadcastSubscription holds only name registration and the runtime watch.
// The channel still uses portEnd's common FIFO, ref and execution-scope lifetime.
type broadcastSubscription struct {
	name    string
	unwatch func() bool
}

// postBroadcast checks the channel before cloning, including when the worker
// has halted: a closed channel still throws rather than silently dropping.
func (m *messaging) postBroadcast(id int, v quickjs.Value) error {
	e := m.ports[id]
	if e == nil || e.broadcast == nil {
		return m.rt.Throw(m.newDOMExceptionOr("BroadcastChannel is closed.", "InvalidStateError"))
	}
	if m.isHalted() {
		return nil
	}
	return e.broadcastMessage(v)
}

// broadcasts is every BroadcastChannel open in the process, by name: those of
// every runtime, which is what lets a worker hear its parent's.
var broadcasts = struct {
	sync.Mutex
	byName map[string]map[*portCore]bool
}{byName: map[string]map[*portCore]bool{}}

// openBroadcast makes obj a BroadcastChannel of a name, returning its id. It
// dispatches what it hears at once, and, as in node, holds the loop until it
// is closed or unref'd.
func (m *messaging) openBroadcast(name string, obj quickjs.Value) int {
	m.next++
	core := &portCore{started: true}
	e := &portEnd{m: m, id: m.next, core: core, obj: obj, reffed: true, broadcast: &broadcastSubscription{name: name}, work: m.startWork()}
	core.owner = e
	m.ports[e.id] = e
	broadcasts.Lock()
	if broadcasts.byName[name] == nil {
		broadcasts.byName[name] = map[*portCore]bool{}
	}
	broadcasts.byName[name][core] = true
	broadcasts.Unlock()
	// A channel the script never closes stops hearing its name when its
	// runtime is closed: until then it kept the runtime reachable, and what
	// was broadcast to it queued up without end.
	e.broadcast.unwatch = context.AfterFunc(loopContext(m.loop), func() { unregisterBroadcast(name, core) })
	if err := e.watchExecution(); err != nil {
		e.closeForExecution()
		return e.id
	}
	e.hold()
	return e.id
}

// broadcastMessage posts a clone of v to every other channel of the name.
func (e *portEnd) broadcastMessage(v quickjs.Value) error {
	broadcasts.Lock()
	var to []*portCore
	for c := range broadcasts.byName[e.broadcast.name] {
		if c != e.core {
			to = append(to, c)
		}
	}
	broadcasts.Unlock()
	// Serialized once, as the standard has it -- a getter in it runs once,
	// however many hear it -- and copied for each: each deserializes its own.
	// What nothing hears is serialized all the same, so what cannot be cloned
	// is still refused.
	data, _, err := e.m.serialize(v, nil, e)
	if err != nil || len(to) == 0 {
		return err
	}
	// Every copy is made before any is sent: one sent may be deserialized,
	// and its bytes moved, while the next is being copied.
	copies := make([]any, len(to))
	copies[0] = data
	for i := 1; i < len(to); i++ {
		copies[i] = data.(*quickjs.Serialized).Copy()
	}
	for i, c := range to {
		c.enqueue(portMsg{data: copies[i]})
	}
	return nil
}

// closeBroadcast stops a BroadcastChannel hearing its name.
func (e *portEnd) closeBroadcast() {
	if e.broadcast.unwatch != nil {
		e.broadcast.unwatch()
	}
	unregisterBroadcast(e.broadcast.name, e.core)
	e.release()
}

// unregisterBroadcast takes a BroadcastChannel out of those of its name and
// drops what it has not dispatched. It is safe to call from any goroutine.
func unregisterBroadcast(name string, core *portCore) {
	broadcasts.Lock()
	if set := broadcasts.byName[name]; set != nil {
		delete(set, core)
		if len(set) == 0 {
			delete(broadcasts.byName, name)
		}
	}
	broadcasts.Unlock()
	core.mu.Lock()
	dropped := core.queue
	core.closed, core.queue = true, nil
	core.mu.Unlock()
	dropMessages(dropped...)
}
