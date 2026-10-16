package agent

import _ "embed"

// ModuleSource is the public ES module source installed by the runtime.
//
//go:embed js/agent.js
var ModuleSource string
