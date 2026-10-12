package runtime

import quickjs "github.com/go-quickjs/go-quickjs"

// asyncResult carries plain Go data until startAsyncWork's completion guard
// permits conversion on the runtime owner. Workers must not create JS values.
type asyncResult interface {
	jsValue(*quickjs.Runtime) any
}

type binaryResult []byte

func (data binaryResult) jsValue(rt *quickjs.Runtime) any {
	return rt.NewBytes(data)
}

type fetchResult struct {
	status int
	body   []byte
}

func (response fetchResult) jsValue(rt *quickjs.Runtime) any {
	return map[string]any{
		"status": response.status,
		"body":   string(response.body),
		"bytes":  rt.NewBytes(response.body),
	}
}
