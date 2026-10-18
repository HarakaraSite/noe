package imageapi

import _ "embed"

// ModuleSource is the public ES module source installed by the runtime.
//
//go:embed js/image.js
var ModuleSource string
