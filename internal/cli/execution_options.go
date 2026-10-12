package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// errExecutionTimeout marks only deadlines introduced by the CLI. A parent
// context may have its own deadline, which must keep the ordinary error code.
var errExecutionTimeout = errors.New("noe execution timeout")

// parseExecutionOptions recognizes execution options before the first operand.
// Remaining arguments are returned unchanged so run can pass script arguments
// through without interpreting them.
func parseExecutionOptions(args []string) (time.Duration, []string, error) {
	var timeout time.Duration
	seenTimeout := false

	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			return timeout, args[i+1:], nil
		}

		var value string
		switch {
		case arg == "--timeout":
			if i+1 >= len(args) {
				return 0, nil, errors.New("noe: --timeout requires a duration")
			}
			value = args[i+1]
			i += 2
		case strings.HasPrefix(arg, "--timeout="):
			value = strings.TrimPrefix(arg, "--timeout=")
			i++
		default:
			return timeout, args[i:], nil
		}

		if seenTimeout {
			return 0, nil, errors.New("noe: --timeout may be specified only once")
		}
		seenTimeout = true

		parsed, err := time.ParseDuration(value)
		if err != nil {
			return 0, nil, fmt.Errorf("noe: invalid --timeout duration %q: %w", value, err)
		}
		if parsed <= 0 {
			return 0, nil, fmt.Errorf("noe: --timeout must be positive (got %q)", value)
		}
		timeout = parsed
	}

	return timeout, nil, nil
}
