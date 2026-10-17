package runtime

import (
	"encoding/json"
	"math"
	"strings"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// exceptionDiagnostic detaches diagnostics while the runtime still owns the
// thrown value. The original error remains available to errors.Is/errors.As.
type exceptionDiagnostic struct {
	cause      error
	message    string
	diagnostic string
}

func (e *exceptionDiagnostic) Error() string { return e.message }
func (e *exceptionDiagnostic) Unwrap() error { return e.cause }

func retainException(err error) error {
	if jsErr, ok := err.(*quickjs.Error); ok {
		return snapshotException(jsErr.Value(), jsErr.Error(), jsErr.Stack(), err)
	}
	return err
}

// Snapshot a small set of diagnostic properties, not arbitrary object graphs.
// In particular, do not stringify custom objects (cycles/toJSON/user code).
func snapshotException(value quickjs.Value, message, stack string, cause error) error {
	diagnostic := message
	if stack != "" {
		diagnostic = stack
	}
	if value.IsObject() {
		details := make(map[string]any)
		for _, key := range []string{"code", "stdout", "stderr"} {
			property, err := value.Get(key)
			if err != nil {
				continue
			}
			switch property.Kind() {
			case quickjs.KindString:
				details[key] = property.String()
			case quickjs.KindBool:
				details[key] = property.Bool()
			case quickjs.KindNumber:
				if n := property.Float(); !math.IsNaN(n) && !math.IsInf(n, 0) {
					details[key] = n
				}
			}
		}
		if len(details) != 0 {
			encoded, _ := json.Marshal(details) // Only finite scalars enter the map.
			diagnostic += "\nexception details: " + string(encoded)
		}
	}
	return &exceptionDiagnostic{cause: cause, message: message, diagnostic: diagnostic}
}

// Diagnostic includes a JavaScript stack and available command-error details.
func Diagnostic(err error) string {
	if retained, ok := err.(*exceptionDiagnostic); ok {
		return retained.diagnostic
	}
	if jsErr, ok := err.(*quickjs.Error); ok {
		return Diagnostic(retainException(jsErr))
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var parts []string
		for _, child := range joined.Unwrap() {
			parts = append(parts, Diagnostic(child))
		}
		return strings.Join(parts, "\n")
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if child := wrapped.Unwrap(); child != nil {
			return strings.Replace(err.Error(), child.Error(), Diagnostic(child), 1)
		}
	}
	return err.Error()
}
