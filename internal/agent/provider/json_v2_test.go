package provider

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"strings"
	"testing"
)

func TestProviderJSONV2RejectsDuplicateNamesAndInvalidUTF8(t *testing.T) {
	invalidUTF8 := append([]byte(`{"value":"`), 0xff, '"', '}')
	inputs := []jsonv1.RawMessage{
		jsonv1.RawMessage(`{"value":1,"value":2}`),
		jsonv1.RawMessage(invalidUTF8),
	}
	for _, input := range inputs {
		if _, err := decodeObject(input); err == nil {
			t.Errorf("decodeObject accepted invalid or ambiguous JSON %q", input)
		}
		if _, err := makeChatRequestBody(Request{Model: "model", Messages: []jsonv1.RawMessage{input}}); err == nil {
			t.Errorf("chat request accepted invalid or ambiguous message %q", input)
		}
	}
}

func TestDecodeObjectPreservesExactGenericNumbers(t *testing.T) {
	const number = "9007199254740993"
	value, err := decodeObject([]byte(`{"value":` + number + `}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := value["value"].(jsonv1.Number); !ok || string(got) != number {
		t.Fatalf("decoded number = %#v; want json.Number(%s)", value["value"], number)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), number) {
		t.Fatalf("encoded JSON lost integer precision: %s", encoded)
	}
}

func TestProviderJSONV2PreservesOptionalOutboundValues(t *testing.T) {
	for _, usage := range []jsonv1.RawMessage{jsonv1.RawMessage(`null`), jsonv1.RawMessage(`{}`), jsonv1.RawMessage(`[]`)} {
		encoded, err := json.Marshal(Response{Usage: usage})
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]jsonv1.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if got, ok := fields["usage"]; !ok || string(got) != string(usage) {
			t.Errorf("usage %s encoded as %s", usage, encoded)
		}
	}

	encoded, err := json.Marshal(Response{})
	if err != nil {
		t.Fatal(err)
	}
	var responseFields map[string]jsonv1.RawMessage
	if err := json.Unmarshal(encoded, &responseFields); err != nil {
		t.Fatal(err)
	}
	if _, ok := responseFields["usage"]; ok {
		t.Fatalf("absent usage was serialized: %s", encoded)
	}

	encoded, err = json.Marshal(Request{MaxTokens: 0})
	if err != nil {
		t.Fatal(err)
	}
	var requestFields map[string]jsonv1.RawMessage
	if err := json.Unmarshal(encoded, &requestFields); err != nil {
		t.Fatal(err)
	}
	if _, ok := requestFields["maxTokens"]; ok {
		t.Fatalf("zero maxTokens was serialized: %s", encoded)
	}

	encoded, err = json.Marshal(ModelInfo{})
	if err != nil {
		t.Fatal(err)
	}
	var catalogFields map[string]jsonv1.RawMessage
	if err := json.Unmarshal(encoded, &catalogFields); err != nil {
		t.Fatal(err)
	}
	if _, ok := catalogFields["contextWindow"]; ok {
		t.Errorf("zero contextWindow was serialized: %s", encoded)
	}
	if _, ok := catalogFields["maxOutputTokens"]; ok {
		t.Errorf("zero maxOutputTokens was serialized: %s", encoded)
	}

	zero := 0
	encoded, err = json.Marshal(chatRequestBody{MaxTokens: &zero})
	if err != nil {
		t.Fatal(err)
	}
	var providerFields map[string]jsonv1.RawMessage
	if err := json.Unmarshal(encoded, &providerFields); err != nil {
		t.Fatal(err)
	}
	if got, ok := providerFields["max_tokens"]; !ok || string(got) != "0" {
		t.Fatalf("explicit pointer-to-zero limit was not preserved: %s", encoded)
	}
}
