package runtime

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	quickjs "github.com/go-quickjs/go-quickjs"
)

const identifier = `[\p{L}_$][\p{L}\p{N}\p{Mn}\p{Mc}\p{Pc}$\x{200C}\x{200D}]*`

var declarationInput = regexp.MustCompile(`(?s)^(const|let|var)\s+(` + identifier + `)\s*=\s*(.+)$`)
var assignmentInput = regexp.MustCompile(`(?s)^(` + identifier + `)\s*=\s*(.+)$`)

type asyncForm struct{ expression, binding string }

// asyncInput recognizes only the agreed await forms. The engine checks the
// expression grammar, including strings, templates, comments and regexes.
func asyncInput(rt *quickjs.Runtime, input string) (*asyncForm, error) {
	form, ok := asyncInputCandidate(input)
	if !ok {
		return nil, nil
	}
	if form.binding != "" {
		if err := rt.CheckSyntax(form.binding + " = undefined"); err != nil {
			return nil, err
		}
	}
	expression, err := normalizeAsyncExpression(rt, form.expression)
	if err != nil {
		return nil, fmt.Errorf("unsupported REPL async input: use one await expression or one simple initializer/assignment")
	}
	form.expression = expression
	if form.binding != "" {
		for i, c := range form.expression {
			if c != ',' {
				continue
			}
			// A complete prefix ends at an outer comma. Closing the expression
			// on the same line also prevents commas in // comments matching.
			if rt.CheckSyntax("async function __noe() { return (\n"+form.expression[:i]+");\n}") == nil {
				return nil, fmt.Errorf("unsupported REPL async input: multiple declarations or assignments")
			}
		}
	}
	return form, nil
}

func asyncInputCandidate(input string) (*asyncForm, bool) {
	source := strings.TrimSpace(input)
	form := &asyncForm{expression: source}
	if m := declarationInput.FindStringSubmatch(source); m != nil {
		expression := strings.TrimSpace(m[3])
		if startsAwait(expression) {
			form.binding, form.expression = m[1]+" "+m[2], expression
			return form, true
		}
	}
	if m := assignmentInput.FindStringSubmatch(source); m != nil {
		expression := strings.TrimSpace(m[2])
		if startsAwait(expression) {
			form.binding, form.expression = m[1], expression
			return form, true
		}
	}
	if !startsAwait(source) {
		return nil, false
	}
	return &asyncForm{expression: source}, true
}

func normalizeAsyncExpression(rt *quickjs.Runtime, expression string) (string, error) {
	check := func(expr string) error {
		return rt.CheckSyntax("async function __noe() { return (\n" + expr + "\n); }")
	}
	if err := check(expression); err == nil {
		return expression, nil
	} else {
		for i := strings.LastIndex(expression, ";"); i >= 0; i = strings.LastIndex(expression[:i], ";") {
			if onlyTrivia(expression[i+1:]) && check(expression[:i]) == nil {
				return expression[:i], nil
			}
		}
		return "", err
	}
}

func startsAwait(source string) bool {
	if !strings.HasPrefix(source, "await") {
		return false
	}
	if len(source) == len("await") {
		return true
	}
	r, _ := utf8.DecodeRuneInString(source[len("await"):])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) && !unicode.In(r, unicode.Pc) && r != '$' && r != '\u200c' && r != '\u200d' && r != '\\'
}

func onlyTrivia(source string) bool {
	for {
		source = strings.TrimSpace(source)
		if source == "" {
			return true
		}
		if strings.HasPrefix(source, "//") {
			if i := strings.IndexByte(source, '\n'); i >= 0 {
				source = source[i+1:]
				continue
			}
			return true
		}
		if strings.HasPrefix(source, "/*") {
			if i := strings.Index(source[2:], "*/"); i >= 0 {
				source = source[i+4:]
				continue
			}
		}
		return false
	}
}
