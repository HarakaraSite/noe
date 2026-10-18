package agent

import ()

// Connection is the capability a child process receives from its parent.
// JSON tags are intentionally stable because the CLI passes this object via an
// environment variable to a future runner.
type Connection struct {
	URL                  string `json:"url"`
	Token                string `json:"token"`
	Scope                string `json:"scope"`
	Defaults             Config `json:"defaults"`
	ModelWindow          *int64 `json:"modelWindow,omitempty"`
	ModelMaxOutputTokens *int64 `json:"modelMaxOutputTokens,omitempty"`
}
