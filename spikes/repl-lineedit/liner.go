package main

import (
	"errors"
	"os"
	"strings"
	"syscall"

	"github.com/peterh/liner"
)

type linerEditor struct {
	l       *liner.State
	cooked  liner.ModeApplier
	editing liner.ModeApplier
	fail    bool
}

func newLiner(hooks, fail bool) (editor, error) {
	cooked, err := liner.TerminalMode()
	if err != nil {
		return nil, err
	}
	l := liner.NewLiner()
	l.SetCtrlCAborts(true)
	l.SetMultiLineMode(true) // visual wrapping of one logical line
	editing, err := liner.TerminalMode()
	if err != nil {
		l.Close()
		return nil, err
	}
	if err := cooked.ApplyMode(); err != nil {
		l.Close()
		return nil, err
	}
	if hooks {
		l.SetCompleter(func(line string) []string {
			var candidates []string
			for _, word := range []string{"console", "constant"} {
				if strings.HasPrefix(word, line) {
					candidates = append(candidates, word)
				}
			}
			return candidates
		})
	}
	return &linerEditor{l: l, cooked: cooked, editing: editing, fail: fail}, nil
}

func (e *linerEditor) readLine(prompt string) (string, error) {
	if err := e.editing.ApplyMode(); err != nil {
		return "", err
	}
	defer e.cooked.ApplyMode()
	if e.fail {
		// Liner reads process fd 0 directly. Temporarily substitute a write-only
		// descriptor to provoke a read error, then restore fd 0 before termios.
		saved, err := syscall.Dup(0)
		if err != nil {
			return "", err
		}
		defer syscall.Close(saved)
		f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if err := syscall.Dup2(int(f.Fd()), 0); err != nil {
			return "", err
		}
		defer syscall.Dup2(saved, 0)
	}
	line, err := e.l.Prompt(prompt)
	if errors.Is(err, liner.ErrPromptAborted) {
		return line, errInterrupt
	}
	if err == nil {
		e.l.AppendHistory(line)
	}
	return line, err
}

func (e *linerEditor) close() error { return e.l.Close() }
