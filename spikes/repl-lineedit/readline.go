package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/chzyer/readline"
)

type readlineEditor struct {
	r      *readline.Instance
	input  *observedInput
	cancel *readline.CancelableStdin
}

func newReadline(hooks, fail bool) (editor, error) {
	input := &observedInput{r: os.Stdin, fail: fail}
	cancel := readline.NewCancelableStdin(input)
	cfg := &readline.Config{
		Prompt: "> ", HistoryLimit: 1000, DisableAutoSaveHistory: false,
		Stdin: cancel, Stdout: os.Stdout, Stderr: os.Stderr,
	}
	var r *readline.Instance
	cfg.FuncFilterInputRune = func(key rune) (rune, bool) {
		// NOE increment 3 exits on Ctrl+C even inside search/completion.
		// This callback runs on readline's operation goroutine, not JS's.
		if key == readline.CharInterrupt {
			if r.Operation.IsSearchMode() {
				r.Operation.ExitSearchMode(true)
			}
			if r.Operation.IsInCompleteMode() {
				r.Operation.ExitCompleteMode(false)
			}
		}
		return key, true
	}
	if hooks {
		cfg.AutoComplete = staticCompletion{}
	}
	var err error
	r, err = readline.NewEx(cfg)
	if err != nil {
		return nil, err
	}
	return &readlineEditor{r: r, input: input, cancel: cancel}, nil
}

func (e *readlineEditor) readLine(prompt string) (string, error) {
	e.r.SetPrompt(prompt)
	line, err := e.r.Readline()
	if errors.Is(err, readline.ErrInterrupt) {
		return line, errInterrupt
	}
	// readline converts all reader errors into EOF; preserve actual input failure.
	if errors.Is(err, io.EOF) {
		if observed := e.input.failure(); observed != nil {
			return line, observed
		}
	}
	return line, err
}

func (e *readlineEditor) close() error {
	// FillableStdin.Close does not close the CancelableStdin it wraps.
	e.cancel.Close()
	return e.r.Close()
}

type staticCompletion struct{}

func (staticCompletion) Do(line []rune, pos int) ([][]rune, int) {
	prefix := string(line[:pos])
	var candidates [][]rune
	for _, word := range []string{"console", "constant"} {
		if strings.HasPrefix(word, prefix) {
			candidates = append(candidates, []rune(word[len(prefix):]))
		}
	}
	return candidates, pos
}

type observedInput struct {
	r    io.Reader
	fail bool
	mu   sync.Mutex
	err  error
}

func (r *observedInput) Read(b []byte) (int, error) {
	var n int
	var err error
	if r.fail {
		err = errors.New("spike input failure")
	} else {
		n, err = r.r.Read(b)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		r.mu.Lock()
		r.err = err
		r.mu.Unlock()
	}
	return n, err
}

func (r *observedInput) failure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}
