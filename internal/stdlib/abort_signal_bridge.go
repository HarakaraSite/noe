package stdlib

import (
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

var abortSignalOperations sync.Map

func bindAbortSignals(rt *quickjs.Runtime, operations quickjs.Value) {
	if _, loaded := abortSignalOperations.Swap(rt, operations); !loaded {
		rt.OnClose(func() { abortSignalOperations.Delete(rt) })
	}
}

// InstallAbortSignalAccess exposes the runtime's internal AbortSignal operations
// to a private builtin factory host. Resolve lazily so independently installed
// builtins can be composed in either order.
func InstallAbortSignalAccess(rt *quickjs.Runtime, host quickjs.Value) error {
	return host.Set("abortSignals", func() any {
		operations, _ := abortSignalOperations.Load(rt)
		return operations
	})
}
