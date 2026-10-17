// Package replui adapts go-quickjs's qjs line editor to an embedding host.
// It owns terminal input and rendering; JavaScript evaluation stays with NOE.
package replui

import (
	"errors"
	"io"
)

const HistoryLimit = 1000

// ErrInterrupt reports Ctrl+C while reading a line.
var ErrInterrupt = errInterrupt

// Input is the qjs-derived terminal frontend. The owner supplies history and
// completion so no JavaScript runtime is accessed by the input goroutine.
type Input struct {
	t  *terminal
	ed *editor
}

// New returns nil when the supplied streams are not usable terminals.
func New(stdin io.Reader, stdout io.Writer) *Input {
	t := openTerminal(stdin, stdout)
	if t == nil {
		return nil
	}
	return &Input{t: t, ed: newEditor(t.in, t.w, t.width)}
}

func (i *Input) ReadLine(prompt string) (line string, err error) {
	if err := i.t.raw(); err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, i.t.restore()) }()
	return i.ed.readLine(prompt)
}

func (i *Input) Close() error { return i.t.close() }

func (i *Input) AddHistory(line string) { i.ed.addHistory(line) }

// SetCompleter receives rune positions and returns replacement candidates for
// line[start:pos]. The host must route runtime access to its owner goroutine.
func (i *Input) SetCompleter(complete func(line []rune, pos int) ([]string, int)) {
	i.ed.complete = complete
}
