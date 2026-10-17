// Adapted from go-quickjs v0.25.3, cmd/qjs. See LICENSE and README.md.
package replui

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableEscapes asks a Windows console to interpret the escape sequences the
// line editor draws with, reporting whether it can.
func enableEscapes(out *os.File) (func() error, bool) {
	h := windows.Handle(out.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return nil, false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return nil, true
	}
	if err := windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		return nil, false
	}
	return func() error { return windows.SetConsoleMode(h, mode) }, true
}
