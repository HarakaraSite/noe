// Adapted from go-quickjs v0.25.3, cmd/qjs. See LICENSE and README.md.
package replui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/term"
)

// terminal is standard input and output when both are a terminal the line
// editor can drive.
type terminal struct {
	in, out *os.File
	w       io.Writer
	// Close may restore the terminal while the input goroutine is blocked.
	mu            sync.Mutex
	cooked        *term.State
	closed        bool
	restoreOutput func() error
}

// openTerminal returns the terminal behind stdin and stdout, or nil when they
// are not one -- a pipe, a file, a test -- or it cannot be driven: a Windows
// console too old to understand escape sequences.
func openTerminal(stdin io.Reader, stdout io.Writer) *terminal {
	in, ok := stdin.(*os.File)
	if !ok || !term.IsTerminal(int(in.Fd())) {
		return nil
	}
	out, ok := fileOf(stdout)
	if !ok || !term.IsTerminal(int(out.Fd())) {
		return nil
	}
	restoreOutput, ok := enableEscapes(out)
	if !ok {
		return nil
	}
	return &terminal{in: in, out: out, w: stdout, restoreOutput: restoreOutput}
}

// fileOf is the terminal file when output has not been redirected or wrapped.
func fileOf(w io.Writer) (*os.File, bool) {
	f, ok := w.(*os.File)
	return f, ok
}

// raw puts the terminal in raw mode, where each key arrives as it is pressed
// and nothing is echoed, and asks it to mark pasted text.
func (t *terminal) raw() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return io.ErrClosedPipe
	}
	st, err := term.MakeRaw(int(t.in.Fd()))
	if err != nil {
		return err
	}
	t.cooked = st
	_, err = fmt.Fprint(t.w, "\x1b[?2004h")
	if err != nil {
		return errors.Join(err, t.restoreLocked())
	}
	return nil
}

// restore puts the terminal back as it was, which is how the code a line runs
// writes to it: with its own line endings, and able to be interrupted.
func (t *terminal) restore() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.restoreLocked()
}

func (t *terminal) restoreLocked() error {
	if t.cooked == nil {
		return nil
	}
	_, writeErr := fmt.Fprint(t.w, "\x1b[?2004l")
	restoreErr := term.Restore(int(t.in.Fd()), t.cooked)
	if restoreErr == nil {
		t.cooked = nil
	}
	return errors.Join(writeErr, restoreErr)
}

func (t *terminal) close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	err := t.restoreLocked()
	if t.restoreOutput != nil {
		err = errors.Join(err, t.restoreOutput())
	}
	return err
}

// width is the terminal's width in columns.
func (t *terminal) width() int {
	w, _, err := term.GetSize(int(t.out.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}
