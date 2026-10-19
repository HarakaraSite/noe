//go:build !linux && !darwin && !windows

package signals

import "os"

var platformSignals = map[string]os.Signal{}
