package jsexec

import (
	"context"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// newProfileVM enables the engine's stack mapping with this execution's
// filesystem authority. The engine owns map parsing and its per-VM cache.
func newProfileVM(ctx context.Context, backend *filesystem.Backend, files bool) (*quickjs.Runtime, context.CancelFunc) {
	mapContext, stopMaps := context.WithCancel(ctx)
	var fs FileSystem
	if files {
		fs = filesystem.WithContext(mapContext, backend)
	}
	rt := quickjs.New(quickjs.WithSourceMaps(func(script, reference string) ([]byte, string, error) {
		return readSourceMap(mapContext, fs, script, reference)
	}))
	rt.OnClose(stopMaps)
	return rt, stopMaps
}
