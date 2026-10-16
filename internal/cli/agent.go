package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"noe/internal/agent"
	"noe/internal/agent/provider"
)

// agentOptions supplies process metadata explicitly; stdlib's process.env is
// an independent snapshot and is not used by the native agent transport.
func agentOptions(stderr io.Writer) (*agent.Options, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("agent executable: %w", err)
	}
	opts := &agent.Options{
		Executable: executable,
		ProviderOptions: provider.Options{
			KeyFile: os.Getenv(provider.DefaultKeyFileEnv),
			BaseURL: os.Getenv("NOE_OPENROUTER_BASE_URL"),
		},
	}
	if os.Getenv("NOE_AGENT_TRACE") == "1" {
		opts.Trace = stderr
	}
	if value, ok := os.LookupEnv("NOE_AGENT_CONNECTION"); ok {
		var connection agent.Connection
		if err := json.Unmarshal([]byte(value), &connection); err != nil {
			return nil, fmt.Errorf("invalid NOE_AGENT_CONNECTION configuration")
		}
		opts.Connection = &connection
	}
	return opts, nil
}
