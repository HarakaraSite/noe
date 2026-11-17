package stdlib

import (
	_ "embed"
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed blob_formdata.js
var blobFormDataJS string

// These callbacks stay private and run on the runtime owner. Late lookup also
// supports embedders installing Network/WebSockets before the Web interfaces.
var binaryDataFactories sync.Map

func bindBinaryData(rt *quickjs.Runtime, callback quickjs.Value) {
	if _, loaded := binaryDataFactories.LoadOrStore(rt, callback); loaded {
		binaryDataFactories.Store(rt, callback)
		return
	}
	rt.OnClose(func() { binaryDataFactories.Delete(rt) })
}

func binaryData(rt *quickjs.Runtime, value quickjs.Value, operation string) (any, error) {
	callback, ok := binaryDataFactories.Load(rt)
	if !ok {
		return nil, nil
	}
	return callback.(quickjs.Value).Call(value, operation)
}
