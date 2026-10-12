package cli

import (
	"bufio"
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
	session, err := runtime.NewSession(runtime.Options{Stdout: stdout, Stderr: stderr})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer session.Close()
	readCtx, stopReading := context.WithCancel(ctx)
	defer stopReading()
	lines := make(chan inputLine)
	go func() {
		reader := bufio.NewReader(stdin)
		for {
			text, err := reader.ReadString('\n')
			select {
			case lines <- inputLine{text: text, err: err}:
			case <-readCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		if ctx.Err() != nil {
			return interruptedCode(ctx, stderr)
		}
		if _, err := fmt.Fprint(stdout, "> "); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		var line inputLine
		select {
		case <-ctx.Done():
			return interruptedCode(ctx, stderr)
		case line = <-lines:
		}
		if ctx.Err() != nil {
			return interruptedCode(ctx, stderr)
		}
		if line.err != nil && !errors.Is(line.err, io.EOF) {
			fmt.Fprintln(stderr, line.err)
			return 1
		}
		input := strings.TrimSuffix(strings.TrimSuffix(line.text, "\n"), "\r")
		if strings.TrimSpace(input) != "" {
			result := session.Eval(ctx, input)
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
