package agent

import (
	_ "embed"
	"strings"
)

// ModuleSource is the public ES module source installed by the runtime.
//
//go:embed js/agent.js
var ModuleSource string

// APIModuleSource contains the shared API factory used by public agent imports
// and invocation-scoped hook APIs.
//
//go:embed js/api.js
var APIModuleSource string

// HooksModuleSource is the private callback-scope helper for configured hooks.
// It remains outside the public noe:agent surface until hook dispatch is added.
//
//go:embed js/hooks.js
var HooksModuleSource string

// TaskModuleSource is the private ES module source used by managed-task
// bootstraps. It is intentionally not part of the documented public API.
//
//go:embed js/task.js
var TaskModuleSource string

//go:embed js/history.js
var HistoryModuleSource string

//go:embed facts.md
var factsMarkdown string

// Facts returns static runtime and API facts without application policy or
// host-supplied instructions.
func Facts() string { return strings.TrimSuffix(factsMarkdown, "\n") }
