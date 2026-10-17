//go:build windows

package processscope

import "errors"

func captureProcessIdentity(pid int) (*ProcessIdentity, error) {
	return nil, errors.New("process identity handles are unavailable on Windows")
}
