// Package executionresult defines detached execution data shared by the runtime
// and agent layers. It has no engine, provider, or host-service dependency.
package executionresult

// Diagnostic separates an observed execution failure from user-written output.
// Code classifies the NOE failure; a user's exception.code belongs in Exception.
type Diagnostic struct {
	Code      string         `json:"code"`
	Stage     string         `json:"stage"`
	Message   string         `json:"message"`
	Stack     string         `json:"stack,omitempty"`
	Position  *Position      `json:"position,omitempty"`
	Exception map[string]any `json:"exception,omitempty"`
}

// Position contains only information actually supplied by the engine. Line and
// column are one-based; missing components are omitted rather than guessed.
type Position struct {
	Source string `json:"source,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

// EncodedBytes preserves binary command output in a JSON diagnostic.
type EncodedBytes struct {
	Encoding string `json:"encoding"`
	Data     string `json:"data"`
}
