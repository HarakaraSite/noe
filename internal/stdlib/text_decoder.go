package stdlib

import (
	_ "embed"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed text_decoder.js
var textDecoderJS string

// newTextDecoder evaluates the TextDecoder implementation and returns its
// constructor. Its caller installs the constructor before composing APIs that
// use TextDecoder.
func newTextDecoder(rt *quickjs.Runtime, host quickjs.Value) (quickjs.Value, error) {
	return evalWithHost(rt, "<text-decoder>", textDecoderJS, host)
}
