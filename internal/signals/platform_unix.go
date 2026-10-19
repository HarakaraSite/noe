//go:build linux || darwin

package signals

import (
	"os"
	"syscall"
)

var platformSignals = map[string]os.Signal{
	"SIGHUP":  syscall.SIGHUP,
	"SIGINT":  os.Interrupt,
	"SIGTERM": syscall.SIGTERM,
	"SIGUSR1": syscall.SIGUSR1,
	"SIGUSR2": syscall.SIGUSR2,
}
