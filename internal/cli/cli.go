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

	"noe/internal/runtime"
)

// Version can be set at build time using -ldflags '-X noe/internal/cli.Version=...'.
var Version = "v1.0.0"

const usage = `Usage:
  noe run [--timeout duration] script.js [args...]
  noe eval [--timeout duration] 'code'
  noe repl
  noe --help
  noe --version

REPL:
  Ctrl+D or noe.exit() exits. Ctrl+C cancels terminal input/evaluation.
  Tab completes names. Unfinished input continues at ... .
  NOE_REPL_HISTORY sets the history file; empty disables persistence.
`

// Run executes a command and returns its process exit code.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}
	var source runtime.Source
	var scriptArgs []string
	var executionTimeout time.Duration
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "--version", "-v", "version":
		fmt.Fprintln(stdout, "noe", Version)
		return 0
	case "repl":
		if len(args) != 1 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return runREPL(ctx, stdin, stdout, stderr)
	case "run":
		timeout, operands, err := parseExecutionOptions(args[1:])
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
		executionTimeout = timeout
	case "eval":
		timeout, operands, err := parseExecutionOptions(args[1:])
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
		executionTimeout = timeout
	default:
		fmt.Fprintf(stderr, "noe: unknown command %q\n%s", args[0], usage)
		return 2
	}
	runtimeOptions := runtime.Options{Stdin: stdin, Stdout: stdout, Stderr: stderr, Args: scriptArgs}
	executionContext := ctx
	var cancel context.CancelFunc
	if executionTimeout > 0 {
		executionContext, cancel = context.WithTimeoutCause(ctx, executionTimeout, errExecutionTimeout)
		defer cancel()
	}
	code, err := runtime.Execute(executionContext, source, runtimeOptions)
	if err != nil {
		if executionTimeout > 0 && errors.Is(err, context.DeadlineExceeded) && errors.Is(context.Cause(executionContext), errExecutionTimeout) {
			fmt.Fprintf(stderr, "%s: execution timed out after %s\n", source.Name, executionTimeout)
			return 124
		}
		fmt.Fprintf(stderr, "%s: %s\n", source.Name, runtime.Diagnostic(err))
		return 1
	}
	return code
}
