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
	options, operands, err := parseCommandOptions(args)
	return options.timeout, operands, err
}

type commandOptions struct {
	timeout   time.Duration
	events    *string // nil disables observation; empty chooses an automatic file name.
	observe   bool    // render human-readable events directly on stdout.
	importMap string  // explicit import-map path; empty disables mappings.
}

func parseCommandOptions(args []string) (commandOptions, []string, error) {
	var options commandOptions
	var timeout time.Duration
	seenTimeout := false

	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			options.timeout = timeout
			return options, args[i+1:], nil
		}
		if arg == "--import-map" || strings.HasPrefix(arg, "--import-map=") {
			if options.importMap != "" {
				return commandOptions{}, nil, errors.New("noe: --import-map may be specified only once")
			}
			value := strings.TrimPrefix(arg, "--import-map=")
			if arg == "--import-map" {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
					return commandOptions{}, nil, errors.New("noe: --import-map requires a file path")
				}
				value = args[i+1]
				i++
			}
			if value == "" {
				return commandOptions{}, nil, errors.New("noe: --import-map requires a file path")
			}
			options.importMap = value
			i++
			continue
		}
		if arg == "--observe" {
			if options.events != nil && *options.events == "-" {
				return commandOptions{}, nil, errors.New("noe: --observe and --events=- both use stdout; use --events=path to save JSONL")
			}
			options.observe = true
			i++
			continue
		}
		if arg == "--events" || strings.HasPrefix(arg, "--events=") {
			if options.events != nil {
				return commandOptions{}, nil, errors.New("noe: --events may be specified only once")
			}
			value := ""
			if arg != "--events" {
				value = strings.TrimPrefix(arg, "--events=")
				if value == "" {
					return commandOptions{}, nil, errors.New("noe: --events= requires a file path or -")
				}
			}
			if options.observe && value == "-" {
				return commandOptions{}, nil, errors.New("noe: --observe and --events=- both use stdout; use --events=path to save JSONL")
			}
			options.events = &value
			i++
			continue
		}

		var value string
		switch {
		case arg == "--timeout":
			if i+1 >= len(args) {
				return commandOptions{}, nil, errors.New("noe: --timeout requires a duration")
			}
			value = args[i+1]
			i += 2
		case strings.HasPrefix(arg, "--timeout="):
			value = strings.TrimPrefix(arg, "--timeout=")
			i++
		default:
			options.timeout = timeout
			return options, args[i:], nil
		}

		if seenTimeout {
			return commandOptions{}, nil, errors.New("noe: --timeout may be specified only once")
		}
		seenTimeout = true

		parsed, err := time.ParseDuration(value)
		if err != nil {
			return commandOptions{}, nil, fmt.Errorf("noe: invalid --timeout duration %q: %w", value, err)
		}
		if parsed <= 0 {
			return commandOptions{}, nil, fmt.Errorf("noe: --timeout must be positive (got %q)", value)
		}
		timeout = parsed
	}

	options.timeout = timeout
	return options, nil, nil
}
