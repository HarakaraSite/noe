package stdlib

import (
	_ "embed"
	"errors"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed fs_watch.js
var fsWatchJS string

func (f *fsHost) installWatchValues() (quickjs.Value, error) {
	host, err := newFSWatchHost(f)
	if err != nil {
		return quickjs.Value{}, err
	}
	native := f.rt.NewObject()
	if err := errors.Join(
		native.Set("open", func(path string, persistent bool, maxQueue int, ignoreOverflow, bytes bool) (quickjs.Value, error) {
			value, err := host.open(path, persistent, maxQueue, ignoreOverflow, bytes)
			return filesystemResult(f, value, err)
		}),
		native.Set("events", func() (quickjs.Value, error) {
			if f.cfg.Loop == nil {
				return quickjs.Value{}, f.throwError(fileError(errors.New("filesystem watch callbacks need an event loop"), "ERR_NOT_SUPPORTED"))
			}
			// Resolve the installed constructor lazily: Files also works in
			// profiles that deliberately omit the pure Node modules.
			ns, err := f.rt.RequireModule("node:events", "")
			if err != nil {
				return quickjs.Value{}, err
			}
			return ns.Get("EventEmitter")
		}),
		native.Set("report", func(reason quickjs.Value) {
			// Callback watches require this loop. Record callback failures
			// without relying on an embedder's promise-rejection policy.
			f.cfg.Loop.fail(f.rt.Throw(reason))
		}),
	); err != nil {
		host.Close()
		return quickjs.Value{}, err
	}
	api, err := evalWithHost(f.rt, "<fs-watch>", fsWatchJS, native)
	if err != nil {
		host.Close()
	}
	return api, err
}
