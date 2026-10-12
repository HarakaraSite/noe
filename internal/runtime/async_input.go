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

var declarationInput = regexp.MustCompile(`^(const|let|var)\s+(` + identifier + `)\s*=\s*(.+)$`)
var assignmentInput = regexp.MustCompile(`^(` + identifier + `)\s*=\s*(.+)$`)

type asyncForm struct{ expression, binding string }

// asyncInput recognizes only the agreed await forms. The engine checks the
// expression grammar, including strings, templates, comments and regexes.
func asyncInput(rt *quickjs.Runtime, input string) (*asyncForm, error) {
	source := strings.TrimSpace(input)
	form := &asyncForm{expression: source}
	if m := declarationInput.FindStringSubmatch(source); m != nil && startsAwait(m[3]) {
		form.binding, form.expression = m[1]+" "+m[2], m[3]
	} else if m := assignmentInput.FindStringSubmatch(source); m != nil && startsAwait(m[2]) {
		form.binding, form.expression = m[1], m[2]
	} else if !startsAwait(source) {
		return nil, nil
	}
	if form.binding != "" {
		if err := rt.CheckSyntax(form.binding + " = undefined"); err != nil {
			return nil, err
		}
	}
	valid := func(expr string) bool {
		return rt.CheckSyntax("async function __noe() { return (\n"+expr+"\n); }") == nil
	}
	if !valid(form.expression) {
		found := false
		for i := strings.LastIndex(form.expression, ";"); i >= 0; i = strings.LastIndex(form.expression[:i], ";") {
			if onlyTrivia(form.expression[i+1:]) && valid(form.expression[:i]) {
				form.expression, found = form.expression[:i], true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unsupported REPL async input: use one await expression or one simple initializer/assignment")
		}
	}
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
