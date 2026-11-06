package stdlib

import (
	_ "embed"

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
	if err := native.Set("open", func(path string, persistent bool, maxQueue int, ignoreOverflow, bytes bool) (quickjs.Value, error) {
		value, err := host.open(path, persistent, maxQueue, ignoreOverflow, bytes)
		return filesystemResult(f, value, err)
	}); err != nil {
		host.Close()
		return quickjs.Value{}, err
	}
	api, err := evalWithHost(f.rt, "<fs-watch>", fsWatchJS, native)
	if err != nil {
		host.Close()
	}
	return api, err
}
