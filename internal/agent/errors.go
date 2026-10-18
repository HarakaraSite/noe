package agent

import (
	"errors"
)

var (
	ErrHostClosed      = errors.New("agent host is closed")
	ErrOperationClosed = errors.New("agent operation is closed")
)
