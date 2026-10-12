package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"noe/internal/runtime"
)

type inputLine struct {
	text string
	err  error
}

func runREPL(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) int {
	if ctx.Err() != nil {
		return interruptedCode(ctx, stderr)
	}
	input, err := newREPLInput(stdin, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return runREPLWithInput(ctx, input, stdout, stderr)
}

func runREPLWithInput(ctx context.Context, input replInput, stdout, stderr io.Writer) (code int) {
	defer func() {
		if err := input.close(); err != nil {
			fmt.Fprintln(stderr, err)
			if code == 0 {
				code = 1
			}
		}
	}()
	session, err := runtime.NewSession(runtime.Options{Stdout: stdout, Stderr: stderr})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer session.Close()
	for {
		if ctx.Err() != nil {
			return interruptedCode(ctx, stderr)
		}
		// Read one input, then evaluate it before starting another read. Liner
		// returns the terminal to cooked mode while JS and host I/O are running.
		lines := make(chan inputLine, 1)
		go func() {
			text, err := input.readLine("> ")
			lines <- inputLine{text: text, err: err}
		}()
		var line inputLine
		select {
		case <-ctx.Done():
			return interruptedCode(ctx, stderr)
		case line = <-lines:
		}
		if ctx.Err() != nil {
			return interruptedCode(ctx, stderr)
		}
		if errors.Is(line.err, errREPLInterrupt) {
			return 130
		}
		if line.err != nil && !errors.Is(line.err, io.EOF) {
			fmt.Fprintln(stderr, line.err)
			return 1
		}
		if strings.TrimSpace(line.text) != "" {
			result := session.Eval(ctx, line.text)
			if ctx.Err() != nil {
				return interruptedCode(ctx, stderr)
			}
			if result.Err != nil {
				fmt.Fprintln(stderr, runtime.Diagnostic(result.Err))
			}
			if result.Exit {
				return result.Code
			}
			if result.Output != "" {
				if _, err := fmt.Fprintln(stdout, result.Output); err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
			}
		}
		if errors.Is(line.err, io.EOF) {
			return 0
		}
	}
}

func interruptedCode(ctx context.Context, stderr io.Writer) int {
	if errors.Is(ctx.Err(), context.Canceled) {
		return 130
	}
	fmt.Fprintln(stderr, ctx.Err())
	return 1
}
