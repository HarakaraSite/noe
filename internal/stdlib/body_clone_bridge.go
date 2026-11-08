package stdlib

import (
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// Body tee factories stay Go-private and run only on the runtime owner. Late
// lookup permits Network and Streams installation in either order.
var bodyTeeFactories sync.Map

func bindBodyTee(rt *quickjs.Runtime, factory quickjs.Value) {
	if _, loaded := bodyTeeFactories.LoadOrStore(rt, factory); loaded {
		bodyTeeFactories.Store(rt, factory)
		return
	}
	rt.OnClose(func() { bodyTeeFactories.Delete(rt) })
}

func teeBody(rt *quickjs.Runtime, stream quickjs.Value) (quickjs.Value, error) {
	factory, ok := bodyTeeFactories.Load(rt)
	if !ok {
		return quickjs.Value{}, rt.Throw(rt.NewError("TypeError", "Body cloning requires the Streams API"))
	}
	return factory.(quickjs.Value).Call(stream)
}
