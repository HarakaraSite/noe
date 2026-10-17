// Package cli implements NOE's command-line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"noe/internal/events"
	"noe/internal/runtime"
)

// Version can be set at build time using -ldflags '-X noe/internal/cli.Version=...'.
var Version = "v1.2.0"

const usage = `Usage:
  noe run [--timeout duration] [--observe] [--events[=path|-]] script.js [args...]
  noe eval [--timeout duration] [--observe] [--events[=path|-]] 'code'
  noe repl [--observe] [--events[=path|-]]
  noe observe [--run id] [--follow] [events.jsonl|-]
  noe observe --json --run id [--wait] events.jsonl
  noe agents
  noe steer <run-id> <instruction>
  noe queue <agent-id> <instruction>
  noe interrupt --run <run-id>
  noe interrupt --agent <agent-id>
  noe --help
  noe --version

REPL:
  Ctrl+D or process.exit() exits. Ctrl+C cancels terminal input/evaluation.
  Tab completes names. Unfinished input continues at ... .
  NOE_REPL_HISTORY sets the history file; empty disables persistence.

Events:
  --observe displays readable events on stdout without a pipe.
  --events creates noe-events-<date>-<time>-<id>.jsonl in the starting cwd.
  --events=path appends JSONL to a file; --events=- writes JSONL to stdout.
`

// Run executes a command and returns its process exit code.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (exitCode int) {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}
	var source runtime.Source
	var scriptArgs []string
	var executionTimeout time.Duration
	var eventOption *string
	var humanOutput bool
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "--version", "-v", "version":
		fmt.Fprintln(stdout, "noe", Version)
		return 0
	case "observe":
		return runObserve(ctx, args[1:], stdin, stdout, stderr)
	case "agents", "steer", "queue", "interrupt":
		return runAgentControl(ctx, args, stdout, stderr)
	case "repl":
		options, operands, err := parseCommandOptions(args[1:])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if len(operands) != 0 || options.timeout != 0 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		observationCtx, observed, err := openObservation(ctx, options.events, options.observe, stdout, stderr)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer func() { exitCode = observed.finish(exitCode, stderr) }()
		observed.emit(events.Event{"type": "execution.started", "command": "repl"})
		return runObservedREPL(observationCtx, stdin, stdout, stderr, observed)
	case "run":
		options, operands, err := parseCommandOptions(args[1:])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if len(operands) < 1 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		name, err := filepath.Abs(operands[0])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if ext := filepath.Ext(name); ext != ".js" && ext != ".mjs" {
			fmt.Fprintln(stderr, "noe: only .js and .mjs files are supported")
			return 1
		}
		data, err := os.ReadFile(name)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		source = runtime.Source{Name: name, Code: string(data)}
		scriptArgs = operands[1:]
		executionTimeout, eventOption = options.timeout, options.events
		humanOutput = options.observe
	case "eval":
		options, operands, err := parseCommandOptions(args[1:])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if len(operands) != 1 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		source = runtime.Source{Name: filepath.Join(cwd, "<eval>.js"), Code: operands[0]}
		executionTimeout, eventOption = options.timeout, options.events
		humanOutput = options.observe
	default:
		fmt.Fprintf(stderr, "noe: unknown command %q\n%s", args[0], usage)
		return 2
	}
	observationCtx, observed, err := openObservation(ctx, eventOption, humanOutput, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { exitCode = observed.finish(exitCode, stderr) }()
	observed.emit(events.Event{"type": "execution.started", "command": args[0]})
	runtimeOptions := runtime.Options{Stdin: stdin, Stdout: stdout, Stderr: stderr, Args: scriptArgs}
	if observed != nil {
		runtimeOptions.Stdout, runtimeOptions.Stderr = observed.stdout, observed.stderr
	}
	agentSettings, err := agentOptions(stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	observed.configureAgent(agentSettings)
	runtimeOptions.Agent = agentSettings
	executionContext := observationCtx
	var cancel context.CancelFunc
	if executionTimeout > 0 {
		executionContext, cancel = context.WithTimeoutCause(observationCtx, executionTimeout, errExecutionTimeout)
		defer cancel()
	}
	code, err := runtime.Execute(executionContext, source, runtimeOptions)
	if err != nil {
		if executionTimeout > 0 && errors.Is(err, context.DeadlineExceeded) && errors.Is(context.Cause(executionContext), errExecutionTimeout) {
			observed.emit(events.Event{"type": "execution.error", "text": fmt.Sprintf("execution timed out after %s", executionTimeout)})
			fmt.Fprintf(stderr, "%s: execution timed out after %s\n", source.Name, executionTimeout)
			return 124
		}
		observed.emit(events.Event{"type": "execution.error", "text": runtime.Diagnostic(err)})
		fmt.Fprintf(stderr, "%s: %s\n", source.Name, runtime.Diagnostic(err))
		return 1
	}
	return code
}
