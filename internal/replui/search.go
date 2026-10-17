package replui

import "strings"

// searchHistory adds NOE's existing Ctrl+R/S controls to the qjs editor.
// All input and rendering stay on the input goroutine, like ordinary editing.
func (e *editor) searchHistory(backward bool) (key, error) {
	prompt := e.prompt
	original, originalPos := append([]rune(nil), e.line...), e.pos
	defer func() { e.prompt = prompt }()
	var query []rune
	index := len(e.history)
	if !backward {
		index = -1
	}
	find := func(start int) bool {
		step := 1
		if backward {
			step = -1
		}
		for i := start; i >= 0 && i < len(e.history); i += step {
			if strings.Contains(e.history[i], string(query)) {
				index = i
				e.line = append(e.line[:0], []rune(e.history[i])...)
				e.pos = len(e.line)
				return true
			}
		}
		return false
	}
	for {
		direction := "forward"
		if backward {
			direction = "reverse"
		}
		e.prompt = []rune("(" + direction + "-i-search)`" + string(visibleText(query)) + "': ")
		e.refresh()
		k, err := e.readKey()
		if err != nil {
			return key{}, err
		}
		switch k.kind {
		case keyRune, keyBackspace:
			if k.kind == keyRune {
				query = append(query, k.r)
			} else if len(query) > 0 {
				query = query[:len(query)-1]
			}
			start := 0
			if backward {
				start = len(e.history) - 1
			}
			if !find(start) {
				// An unmatched query must not execute a stale earlier match.
				e.line, e.pos = e.line[:0], 0
				index = len(e.history)
				if !backward {
					index = -1
				}
			}
		case keySearchBack:
			backward = true
			find(index - 1)
		case keySearchForward:
			backward = false
			find(index + 1)
		case keyCancelSearch:
			e.line, e.pos = original, originalPos
			return key{kind: keyNone}, nil
		default:
			// Enter runs the selected input; editing keys accept it and edit.
			return k, nil
		}
	}
}
