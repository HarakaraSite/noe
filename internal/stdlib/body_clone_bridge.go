package stdlib

import (
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

type bodyStreamBindings struct {
	tee    quickjs.Value
	reader quickjs.Value
}

// Body stream operations stay private and run only on the runtime owner. Late
// lookup permits Network and Streams installation in either order.
var bodyStreamOperations sync.Map

func bindBodyStreams(rt *quickjs.Runtime, tee, reader quickjs.Value) {
	if _, loaded := bodyStreamOperations.Swap(rt, bodyStreamBindings{tee: tee, reader: reader}); !loaded {
		rt.OnClose(func() { bodyStreamOperations.Delete(rt) })
	}
}

func teeBody(rt *quickjs.Runtime, stream quickjs.Value) (quickjs.Value, error) {
	bindings, ok := bodyStreamOperations.Load(rt)
	if !ok {
		return quickjs.Value{}, rt.Throw(rt.NewError("TypeError", "Body cloning requires the Streams API"))
	}
	return bindings.(bodyStreamBindings).tee.Call(stream)
}

func bodyReaderOperations(rt *quickjs.Runtime) any {
	bindings, ok := bodyStreamOperations.Load(rt)
	if !ok {
		return nil
	}
	return bindings.(bodyStreamBindings).reader
}
