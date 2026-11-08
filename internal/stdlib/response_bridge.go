package stdlib

import (
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// Response operations are private to the runtime owner. Late lookup permits
// embedders to install Network, Servers and Sockets in either order.
var responseFactories sync.Map

type responseFactory struct {
	upgrade, snapshot quickjs.Value
}

func bindResponses(rt *quickjs.Runtime, upgrade, snapshot quickjs.Value) {
	factory := responseFactory{upgrade: upgrade, snapshot: snapshot}
	if _, loaded := responseFactories.LoadOrStore(rt, factory); loaded {
		responseFactories.Store(rt, factory)
		return
	}
	rt.OnClose(func() { responseFactories.Delete(rt) })
}

func upgradeResponse(rt *quickjs.Runtime) (quickjs.Value, error) {
	factory, ok := responseFactories.Load(rt)
	if !ok {
		return quickjs.Value{}, rt.Throw(rt.NewError("TypeError", "WebSocket upgrades require the Response API"))
	}
	return factory.(responseFactory).upgrade.Call()
}

func snapshotResponse(rt *quickjs.Runtime, response quickjs.Value) (quickjs.Value, error) {
	factory, ok := responseFactories.Load(rt)
	if !ok {
		return quickjs.Value{}, rt.Throw(rt.NewError("TypeError", "HTTP replies require the Response API"))
	}
	return factory.(responseFactory).snapshot.Call(response)
}
