package decision

import "testing"

func TestParseRequest(t *testing.T) {
	for _, state := range []string{`"text"`, `{"value":1e3}`, `["text",{"part":true}]`} {
		raw := `{"provider":"openrouter","model":"test/model","state":` + state + `,"questions":{"q":{"type":"future","extension":{"keep":true}}}}`
		r, err := ParseRequest([]byte(raw))
		if err != nil || string(r.State) != state || string(r.Questions["q"]) != `{"type":"future","extension":{"keep":true}}` {
			t.Fatalf("request=%+v err=%v", r, err)
		}
	}
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"provider":"openrouter","model":"m","state":null,"questions":{"q":{}}}`,
		`{"provider":"openrouter","model":"m","state":" ","questions":{"q":{}}}`,
		`{"provider":"openrouter","model":"m","state":true,"questions":{"q":{}}}`,
		`{"provider":"openrouter","model":"m","state":"x","questions":{}}`,
		`{"provider":"openrouter","model":"m","state":"x","questions":{"q":null}}`,
		`{"provider":"openrouter","model":"m","state":"x","questions":{"q":{}},"effort":"auto"}`,
		`{"provider":"openrouter","model":"m","state":"x","questions":{"q":{}}} {}`,
	} {
		if _, err := ParseRequest([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
