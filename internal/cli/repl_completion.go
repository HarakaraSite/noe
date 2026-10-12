package cli

import (
	"context"
	"unicode"

	"github.com/peterh/liner"
)

type completionRequest struct {
	token string
	reply chan []string
}

func connectCompletion(ctx context.Context, input *linerREPLInput, requests chan<- completionRequest) {
	input.state.SetTabCompletionStyle(liner.TabPrints)
	input.state.SetWordCompleter(func(line string, pos int) (string, []string, string) {
		head, token, tail := completionWord(line, pos)
		request := completionRequest{token: token, reply: make(chan []string, 1)}
		select {
		case requests <- request:
		case <-ctx.Done():
			return head, nil, tail
		}
		select {
		case candidates := <-request.reply:
			return head, candidates, tail
		case <-ctx.Done():
			return head, nil, tail
		}
	})
}

// Liner indexes positions by rune. Keep both the source before the current
// identifier chain and the source after the cursor untouched.
func completionWord(line string, pos int) (head, token, tail string) {
	runes := []rune(line)
	if pos < 0 || pos > len(runes) {
		return line, "", ""
	}
	start := pos
	for start > 0 {
		r := runes[start-1]
		if r != '.' && r != '$' && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) && !unicode.In(r, unicode.Pc) && r != '\u200c' && r != '\u200d' {
			break
		}
		start--
	}
	return string(runes[:start]), string(runes[start:pos]), string(runes[pos:])
}
