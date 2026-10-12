package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/peterh/liner"
	"golang.org/x/term"
)

var errREPLInterrupt = errors.New("REPL input interrupted")

// replInput owns input and terminal state, but never touches the JS runtime.
type replInput interface {
	readLine(string) (string, error)
	close() error
}

func newREPLInput(stdin io.Reader, stdout io.Writer) (replInput, error) {
	plain := &plainREPLInput{reader: bufio.NewReader(stdin), out: stdout}
	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	// Liner controls process fd 0 and 1, so borrowed or redirected streams
	// retain the plain input path even if another descriptor names a terminal.
	if !inOK || !outOK || in.Fd() != 0 || out.Fd() != 1 ||
		!term.IsTerminal(0) || !term.IsTerminal(1) || !liner.TerminalSupported() {
		return plain, nil
	}
	cooked, err := liner.TerminalMode()
	if err != nil {
		return plain, nil
	}
	state := liner.NewLiner()
	state.SetCtrlCAborts(true)
	state.SetMultiLineMode(true) // wrap a single logical input across screen rows
	editing, err := liner.TerminalMode()
	if err != nil {
		state.Close()
		if restoreErr := cooked.ApplyMode(); restoreErr != nil {
			return nil, errors.Join(err, restoreErr)
		}
		return plain, nil
	}
	if err := cooked.ApplyMode(); err != nil {
		state.Close()
		return nil, err
	}
	return &linerREPLInput{state: state, cooked: cooked, editing: editing}, nil
}

type plainREPLInput struct {
	reader *bufio.Reader
	out    io.Writer
}

func (p *plainREPLInput) readLine(prompt string) (string, error) {
	if _, err := fmt.Fprint(p.out, prompt); err != nil {
		return "", err
	}
	text, err := p.reader.ReadString('\n')
	return strings.TrimSuffix(strings.TrimSuffix(text, "\n"), "\r"), err
}

func (*plainREPLInput) close() error { return nil }

type linerREPLInput struct {
	state   *liner.State
	cooked  liner.ModeApplier
	editing liner.ModeApplier
}

func (l *linerREPLInput) readLine(prompt string) (text string, err error) {
	if err := l.editing.ApplyMode(); err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, l.cooked.ApplyMode()) }()
	text, err = l.state.Prompt(prompt)
	if errors.Is(err, liner.ErrPromptAborted) {
		return text, errREPLInterrupt
	}
	if err == nil && strings.TrimSpace(text) != "" {
		l.state.AppendHistory(text)
	}
	return text, err
}

func (l *linerREPLInput) close() error {
	// Liner's Close restores its saved mode; keep restoration errors visible.
	return errors.Join(l.state.Close(), l.cooked.ApplyMode())
}
