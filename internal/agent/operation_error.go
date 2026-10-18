package agent

import "errors"

type operationError struct {
	status  int
	code    string
	message string
}

func (e *operationError) Error() string { return e.message }

func newOperationError(status int, code, message string) *operationError {
	return &operationError{status: status, code: code, message: message}
}

var errRequestCancelled = errors.New("agent request cancelled")
