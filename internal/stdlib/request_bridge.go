package stdlib

import (
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// Request factories remain Go-private and are called only by the runtime owner.
// The registry permits Network and Servers to be installed in either order.
var incomingRequestFactories sync.Map

func bindIncomingRequest(rt *quickjs.Runtime, factory quickjs.Value) {
	if _, loaded := incomingRequestFactories.LoadOrStore(rt, factory); loaded {
		incomingRequestFactories.Store(rt, factory)
		return
	}
	rt.OnClose(func() { incomingRequestFactories.Delete(rt) })
}

func incomingRequest(rt *quickjs.Runtime, url, init quickjs.Value) (quickjs.Value, error) {
	factory, ok := incomingRequestFactories.Load(rt)
	if !ok {
		return quickjs.Value{}, rt.Throw(rt.NewError("TypeError", "HTTP requests require the Request API"))
	}
	return factory.(quickjs.Value).Call(url, init)
}
