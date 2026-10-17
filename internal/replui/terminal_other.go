//go:build !windows

// Adapted from go-quickjs v0.25.3, cmd/qjs. See LICENSE and README.md.
package replui

import "os"

// enableEscapes reports whether a terminal interprets escape sequences, which
// every terminal outside Windows does.
func enableEscapes(*os.File) (func() error, bool) { return nil, true }
