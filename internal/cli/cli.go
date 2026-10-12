// Package cli implements NOE's command-line interface.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"noe/internal/runtime"
)

// Version can be set at build time using -ldflags '-X noe/internal/cli.Version=...'.
var Version = "dev"

const usage = `Usage:
  noe run script.js [args...]
  noe eval 'code'
  noe repl
  noe --help
  noe --version
`

// Run executes a command and returns its process exit code.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}
	var source runtime.Source
	var scriptArgs []string
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
		if len(args) < 2 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		name, err := filepath.Abs(args[1])
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
		scriptArgs = args[2:]
	case "eval":
		if len(args) != 2 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		source = runtime.Source{Name: filepath.Join(cwd, "<eval>.js"), Code: args[1]}
	default:
		fmt.Fprintf(stderr, "noe: unknown command %q\n%s", args[0], usage)
		return 2
	}
	code, err := runtime.Execute(ctx, source, runtime.Options{Stdout: stdout, Stderr: stderr, Args: scriptArgs})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %s\n", source.Name, runtime.Diagnostic(err))
		return 1
	}
	return code
}
