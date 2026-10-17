package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"noe/internal/replui"
)

// Each JSON string occupies one physical line, even for multiline JS. Append
// rather than rewrite so separate REPL processes keep one another's inputs.
type replHistory struct {
	path   string
	stderr io.Writer
	failed bool
}

func newREPLHistory(stderr io.Writer) *replHistory {
	path, set := os.LookupEnv("NOE_REPL_HISTORY")
	if !set {
		dir, err := os.UserConfigDir()
		if err != nil {
			fmt.Fprintln(stderr, "noe: history:", err)
			return &replHistory{stderr: stderr}
		}
		path = filepath.Join(dir, "noe", "repl_history.jsonl")
	}
	return &replHistory{path: path, stderr: stderr}
}

func (h *replHistory) load() []string {
	if h.path == "" {
		return nil
	}
	file, err := os.Open(h.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		h.warn(err)
		return nil
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var items []string
	corrupt := false
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var input string
			if parseErr := json.Unmarshal(line, &input); parseErr != nil {
				corrupt = true
			} else if strings.TrimSpace(input) != "" {
				items = append(items, input)
				if len(items) > replui.HistoryLimit {
					items = items[1:]
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			h.warn(err)
			break
		}
	}
	if corrupt {
		h.warn(errors.New("invalid history entries skipped"))
	}
	return items
}

func (h *replHistory) append(input string) {
	if h.path == "" || h.failed {
		return
	}
	data, err := json.Marshal(input)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(h.path), 0700)
	}
	if err == nil {
		var file *os.File
		file, err = os.OpenFile(h.path, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0600)
		if err == nil {
			stat, statErr := file.Stat()
			err = statErr
			if err == nil && stat.Size() > 0 {
				last := make([]byte, 1)
				_, err = file.ReadAt(last, stat.Size()-1)
				if err == nil && last[0] != '\n' {
					data = append([]byte{'\n'}, data...)
				}
			}
			if err == nil {
				_, err = file.Write(append(data, '\n'))
			}
			err = errors.Join(err, file.Close())
		}
	}
	if err != nil {
		h.warn(err)
		h.failed = true
	}
}

func (h *replHistory) warn(err error) { fmt.Fprintln(h.stderr, "noe: history:", err) }
