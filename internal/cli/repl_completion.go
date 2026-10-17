package cli

import (
	"context"
	"unicode"
)

type completionRequest struct {
	token string
	reply chan []string
}

func connectCompletion(ctx context.Context, input *terminalREPLInput, requests chan<- completionRequest) {
	input.editor.SetCompleter(func(line []rune, pos int) ([]string, int) {
		start := completionStart(line, pos)
		token := string(line[start:pos])
		request := completionRequest{token: token, reply: make(chan []string, 1)}
		select {
		case requests <- request:
		case <-ctx.Done():
			return nil, start
		}
		select {
		case candidates := <-request.reply:
			return candidates, start
		case <-ctx.Done():
			return nil, start
		}
	})
}

// The editor indexes positions by rune and preserves the source outside this
// identifier chain, including everything after the cursor.
func completionStart(runes []rune, pos int) int {
	start := pos
	for start > 0 {
		r := runes[start-1]
		if r != '.' && r != '$' && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) && !unicode.In(r, unicode.Pc) && r != '\u200c' && r != '\u200d' {
			break
		}
		start--
	}
	return start
}
