package stdlib

import (
	_ "embed"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed text_streams.js
var textStreamsJS string

// newTextStreams evaluates the TextEncoderStream and TextDecoderStream
// implementations and returns their constructors.
func newTextStreams(rt *quickjs.Runtime, host quickjs.Value) (quickjs.Value, error) {
	return evalWithHost(rt, "<text-streams>", textStreamsJS, host)
}
