package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

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
	ctx, stopRequests := context.WithCancel(ctx)
	defer stopRequests()
	edited, interactive := input.(*linerREPLInput)
	if interactive {
		edited.loadHistory(stderr)
	}
	defer func() {
		if err := input.close(); err != nil {
			fmt.Fprintln(stderr, err)
			if code == 0 {
				code = 1
			}
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	session, err := runtime.NewSession(runtime.Options{Stdout: stdout, Stderr: stderr})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer session.Close()
	requests := make(chan completionRequest)
	if interactive {
		connectCompletion(ctx, edited, requests)
	}
	held := ""
	for {
		if ctx.Err() != nil {
			return interruptedCode(ctx, stderr)
		}
		// Read once, then evaluate in cooked mode before reading again.
		lines := make(chan inputLine, 1)
		prompt := "> "
		if held != "" {
			prompt = "... "
		}
		go func() {
			text, err := input.readLine(prompt)
			lines <- inputLine{text: text, err: err}
		}()
		var line inputLine
	readLoop:
		for {
			select {
			case <-ctx.Done():
				return interruptedCode(ctx, stderr)
			case <-signals:
				return 130 // external SIGINT while waiting for input
			case line = <-lines:
				break readLoop
			case request := <-requests:
				completionCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				candidates, err := session.Complete(completionCtx, request.token)
				cancel()
				var termination *runtime.TerminationError
				if errors.As(err, &termination) {
					stopRequests()
					if termination.Cause != nil {
						fmt.Fprintln(stderr, runtime.Diagnostic(termination.Cause))
					}
					return termination.Code
				}
				if err != nil {
					candidates = nil
				}
				request.reply <- candidates
			}
		}
		if ctx.Err() != nil {
			return interruptedCode(ctx, stderr)
		}
		if errors.Is(line.err, errREPLInterrupt) {
			if !interactive {
				return 130
			}
			held = ""
			fmt.Fprintln(stderr, "Interrupted")
			continue
		}
		if line.err != nil && !errors.Is(line.err, io.EOF) {
			fmt.Fprintln(stderr, line.err)
			return 1
		}
		if held != "" {
			held += "\n"
		}
		held += line.text
		if strings.TrimSpace(held) != "" {
			if !errors.Is(line.err, io.EOF) && session.Incomplete(held) {
				continue
			}
			if history, ok := input.(interface{ remember(string) }); ok {
				history.remember(held)
			}
			result, interrupted := evaluateREPL(ctx, session, signals, held)
			held = ""
			if ctx.Err() != nil {
				return interruptedCode(ctx, stderr)
			}
			if result.Exit {
				if result.Err != nil {
					fmt.Fprintln(stderr, runtime.Diagnostic(result.Err))
				}
				return result.Code
			}
			if interrupted {
				if !interactive {
					return 130
				}
				fmt.Fprintln(stderr, "Interrupted")
				continue
			}
			if result.Err != nil {
				fmt.Fprintln(stderr, runtime.Diagnostic(result.Err))
			}
			if result.Output != "" {
				if _, err := fmt.Fprintln(stdout, result.Output); err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
			}
		} else {
			held = ""
		}
		if errors.Is(line.err, io.EOF) {
			return 0
		}
	}
}

// Only cancellation crosses goroutines: the Session stays on its owner.
func evaluateREPL(ctx context.Context, session *runtime.Session, signals <-chan os.Signal, source string) (runtime.Evaluation, bool) {
	inputCtx, cancel := context.WithCancel(ctx)
	finished, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-signals:
			cancel()
		case <-finished:
		}
	}()
	result := session.Eval(inputCtx, source)
	close(finished)
	<-stopped
	interrupted := inputCtx.Err() != nil
	cancel()
	return result, interrupted
}

func interruptedCode(ctx context.Context, stderr io.Writer) int {
	if errors.Is(ctx.Err(), context.Canceled) {
		return 130
	}
	fmt.Fprintln(stderr, ctx.Err())
	return 1
}
