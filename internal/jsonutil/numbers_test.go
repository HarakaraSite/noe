package jsonutil

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"testing"
)

func TestPreserveNumbers(t *testing.T) {
	var got map[string]any
	if err := json.Unmarshal([]byte(`{"integer":9007199254740993,"nested":[1.2300e+4,{"value":-0}],"text":"ok","flag":true,"nil":null}`), &got, PreserveNumbers()); err != nil {
		t.Fatal(err)
	}
	if got["integer"] != jsonv1.Number("9007199254740993") {
		t.Fatalf("large integer was rounded: %#v", got["integer"])
	}
	nested := got["nested"].([]any)
	if nested[0] != jsonv1.Number("1.2300e+4") || nested[1].(map[string]any)["value"] != jsonv1.Number("-0") {
		t.Fatalf("number spelling changed: %#v", nested)
	}
	if got["text"] != "ok" || got["flag"] != true || got["nil"] != nil {
		t.Fatalf("non-number values changed: %#v", got)
	}
}

func TestPreserveNumbersKeepsStrictSyntax(t *testing.T) {
	for _, input := range []string{`{"a":1,"a":2}`, "{\"text\":\"\xff\"}", `{"a":1} {"b":2}`} {
		var got any
		if err := json.Unmarshal([]byte(input), &got, PreserveNumbers()); err == nil {
			t.Fatalf("accepted invalid or ambiguous JSON: %q", input)
		}
	}
}
