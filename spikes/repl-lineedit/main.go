// This spike connects two line editors to NOE's existing Session.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"noe/internal/runtime"

	"golang.org/x/term"
)

var errInterrupt = errors.New("editor interrupt")

type editor interface {
	readLine(string) (string, error)
	close() error
}

type lineResult struct {
	text string
	err  error
}

type event struct {
	Input  string
	Output string
	Error  string
	Exit   bool
	Code   int
}

func main() {
	name := flag.String("editor", "readline", "readline or liner")
	trace := flag.String("trace", "", "JSONL comparison evidence")
	hooks := flag.Bool("hooks", false, "probe prompt changes and static completion")
	failInput := flag.Bool("fail-input", false, "inject an input failure after entering editing")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, *name, *trace, *hooks, *failInput)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, name, trace string, hooks, failInput bool) int {
	var evidence *json.Encoder
	if trace != "" {
		f, err := os.Create(trace)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer f.Close()
		evidence = json.NewEncoder(f)
	}
	var in editor = &plainEditor{reader: bufio.NewReader(os.Stdin)}
	if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("TERM") != "dumb" {
		var err error
		switch name {
		case "readline":
			in, err = newReadline(hooks, failInput)
		case "liner":
			in, err = newLiner(hooks, failInput)
		default:
			err = fmt.Errorf("unknown editor %q", name)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	defer in.close()
	session, err := runtime.NewSession(runtime.Options{Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer session.Close()
	prompt := "> "
	for {
		// Only one read is started, and the next starts after Eval returns.
		line := make(chan lineResult, 1)
		go func() {
			text, err := in.readLine(prompt)
			line <- lineResult{text: text, err: err}
		}()
		var input lineResult
		select {
		case <-ctx.Done():
			return 130
		case input = <-line:
		}
		if ctx.Err() != nil || errors.Is(input.err, errInterrupt) {
			return 130
		}
		if input.err != nil && !errors.Is(input.err, io.EOF) {
			fmt.Fprintln(os.Stderr, input.err)
			return 1
		}
		if strings.TrimSpace(input.text) != "" {
			result := session.Eval(ctx, input.text)
			if evidence != nil {
				e := event{Input: input.text, Output: result.Output, Exit: result.Exit, Code: result.Code}
				if result.Err != nil {
					e.Error = runtime.Diagnostic(result.Err)
				}
				if err := evidence.Encode(e); err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 1
				}
			}
			if ctx.Err() != nil {
				return 130
			}
			if result.Err != nil {
				fmt.Fprintln(os.Stderr, runtime.Diagnostic(result.Err))
			}
			if result.Exit {
				return result.Code
			}
			if result.Output != "" {
				fmt.Fprintln(os.Stdout, result.Output)
			}
			if hooks {
				prompt = "... "
			}
		}
		if errors.Is(input.err, io.EOF) {
			return 0
		}
	}
}

type plainEditor struct{ reader *bufio.Reader }

func (p *plainEditor) readLine(prompt string) (string, error) {
	fmt.Fprint(os.Stdout, prompt)
	line, err := p.reader.ReadString('\n')
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), err
}

func (p *plainEditor) close() error { return nil }
