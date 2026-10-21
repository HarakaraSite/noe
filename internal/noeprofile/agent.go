package noeprofile

import (
	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"io"
)

// AgentOptions belongs to NOE's application assembly. Model definitions and
// credentials are supplied explicitly to model.Service after NOE selects paths.
type AgentOptions struct {
	Models        model.Options
	Supervisor    string
	ControlDir    string
	EnableControl bool
	Trace         io.Writer
	Observe       func(agent.Event)
}
