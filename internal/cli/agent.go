package cli

import (
	"fmt"
	"io"
	"os"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/noeprofile"
)

// agentOptions supplies process metadata explicitly; stdlib's process.env is
// an independent snapshot and is not used by the native agent transport.
func agentOptions(stderr io.Writer) (*noeprofile.AgentOptions, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("agent executable: %w", err)
	}
	opts := &noeprofile.AgentOptions{
		Supervisor: executable,
		Models: model.Options{
			UserAgent: "noe/" + Version,
			KeyFile:   os.Getenv(provider.DefaultKeyFileEnv),
			BaseURL:   os.Getenv("NOE_OPENROUTER_BASE_URL"),
		},
	}
	if os.Getenv("NOE_AGENT_TRACE") == "1" {
		opts.Trace = stderr
	}
	opts.EnableControl = true

	return opts, nil
}
