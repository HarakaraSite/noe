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

	"forge.harakara.site/littleisland/noe/v2/internal/apidocs"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"
	"forge.harakara.site/littleisland/noe/v2/internal/runtime"
)

// Version can be set at build time using -ldflags '-X forge.harakara.site/littleisland/noe/v2/internal/cli.Version=...'.
var Version = "v2.0.0"

const usage = `Usage:
  noe run [--timeout duration] [--import-map path] [--observe] [--events[=path|-]] script.js [args...]
  noe eval [--timeout duration] [--import-map path] [--observe] [--events[=path|-]] 'code'
  noe observe [--run id] [--follow] [events.jsonl|-]
  noe observe --json --run id [--wait] events.jsonl
  noe agents [--full]
  noe steer <run-id> <instruction>
  noe queue <agent-id> <instruction>
  noe interrupt --run <run-id>
  noe interrupt --agent <agent-id>
  noe docs [topic [member]]
  noe --help
  noe --version

Events:
  --observe displays readable events on stdout without a pipe.
  --events creates .noe/events/noe-events-<date>-<time>-<id>.jsonl under the starting cwd.
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
	var importMapPath string
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "--version", "-v", "version":
		fmt.Fprintln(stdout, "noe", Version)
		return 0
	case "docs":
		return runDocs(args[1:], stdout, stderr)
	case "observe":
		return runObserve(ctx, args[1:], stdin, stdout, stderr)
	case "agents", "steer", "queue", "interrupt":
		return runAgentControl(ctx, args, stdout, stderr)
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
		importMapPath = options.importMap
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
		importMapPath = options.importMap
	default:
		fmt.Fprintf(stderr, "noe: unknown command %q\n%s", args[0], usage)
		return 2
	}
	importMap, err := loadImportMap(importMapPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	observationCtx, observed, err := openObservation(ctx, eventOption, humanOutput, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { exitCode = observed.finish(exitCode, stderr) }()
	observed.emit(events.Event{"type": "execution.started", "command": args[0]})
	runtimeOptions := runtime.Options{Stdin: stdin, Stdout: stdout, Stderr: stderr, Args: scriptArgs, ImportMap: importMap}
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

func runDocs(args []string, stdout, stderr io.Writer) int {
	const help = "Usage: noe docs [topic [member]]\n\nRead the API reference bundled with this executable. No arguments lists topics.\nExamples: noe docs node:fs; noe docs noe:sockets connect\n"
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(stdout, help)
		return 0
	}
	if len(args) > 2 {
		fmt.Fprint(stderr, help)
		return 2
	}
	var topic, member string
	if len(args) > 0 {
		topic = args[0]
	}
	if len(args) > 1 {
		member = args[1]
	}
	text, err := apidocs.Lookup(topic, member)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	fmt.Fprint(stdout, text)
	return 0
}

func loadImportMap(path string) (*modulemap.Snapshot, error) {
	if path == "" {
		return nil, nil
	}
	return modulemap.Load(path)
}
