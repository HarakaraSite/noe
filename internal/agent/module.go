package agent

import (
	_ "embed"
	"strings"
)

// ModuleSource is the public ES module source installed by the runtime.
//
//go:embed js/agent.js
var ModuleSource string

// TaskModuleSource is the private ES module source used by managed-task
// bootstraps. It is intentionally not part of the documented public API.
//go:embed js/task.js
var TaskModuleSource string

//go:embed instructions.md
var instructionMarkdown string

// Instructions returns the shared model instructions embedded in the binary.
// Ignore the Markdown file's final newline when composing the system message.
func Instructions() string { return strings.TrimSuffix(instructionMarkdown, "\n") }
