package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"noe/internal/replui"
)

var errREPLInterrupt = replui.ErrInterrupt

// replInput owns input and terminal state, but never touches the JS runtime.
type replInput interface {
	readLine(string) (string, error)
	close() error
}

func newREPLInput(stdin io.Reader, stdout io.Writer) (replInput, error) {
	plain := &plainREPLInput{reader: bufio.NewReader(stdin), out: stdout}
	if os.Getenv("TERM") == "dumb" {
		return plain, nil
	}
	editor := replui.New(stdin, stdout)
	if editor == nil {
		return plain, nil
	}
	return &terminalREPLInput{editor: editor}, nil
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

type terminalREPLInput struct {
	editor  *replui.Input
	history *replHistory
}

func (l *terminalREPLInput) loadHistory(stderr io.Writer) {
	l.history = newREPLHistory(stderr)
	for _, text := range l.history.load() {
		l.editor.AddHistory(text)
	}
}

func (l *terminalREPLInput) remember(text string) {
	l.editor.AddHistory(text)
	if l.history != nil {
		l.history.append(text)
	}
}

func (l *terminalREPLInput) readLine(prompt string) (string, error) {
	return l.editor.ReadLine(prompt)
}

func (l *terminalREPLInput) close() error { return l.editor.Close() }
