//go:build windows

package signals

import "os"

var platformSignals = map[string]os.Signal{
	"SIGINT": os.Interrupt,
}
