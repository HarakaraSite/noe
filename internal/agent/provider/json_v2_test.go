package provider

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"errors"
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
		for name, build := range requestBodyBuilders() {
			for _, request := range []Request{
				{Model: "model", Messages: []jsonv1.RawMessage{input}},
				{Model: "model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"hello"}`)}, Tools: []jsonv1.RawMessage{input}},
			} {
				if _, err := build(request); err == nil {
					t.Errorf("%s request accepted invalid or ambiguous JSON %q", name, input)
				}
			}
		}
	}
}

func requestBodyBuilders() map[string]func(Request) ([]byte, error) {
	return map[string]func(Request) ([]byte, error){
		"chat": makeChatRequestBody,
		"responses": func(request Request) ([]byte, error) {
			return configuredResponsesRequestBody(Definition{EffortField: "reasoning.effort"}, request)
		},
	}
}

func TestGenerationRequestValidation(t *testing.T) {
	message := []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"hello"}`)}
	invalidUTF8 := string([]byte{0xff})
	cases := []struct {
		name    string
		request Request
	}{
		{"empty model", Request{Messages: message}},
		{"blank model", Request{Model: " \t", Messages: message}},
		{"invalid model UTF-8", Request{Model: invalidUTF8, Messages: message}},
		{"invalid cache key UTF-8", Request{Model: "model", Messages: message, CacheKey: invalidUTF8}},
		{"empty messages", Request{Model: "model"}},
		{"null message", Request{Model: "model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`null`)}}},
		{"array message", Request{Model: "model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`[]`)}}},
		{"malformed message", Request{Model: "model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{`)}}},
		{"null tool", Request{Model: "model", Messages: message, Tools: []jsonv1.RawMessage{jsonv1.RawMessage(`null`)}}},
		{"negative token limit", Request{Model: "model", Messages: message, MaxTokens: -1}},
		{"invalid effort", Request{Model: "model", Messages: message, Effort: "invalid"}},
	}
	for name, build := range requestBodyBuilders() {
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				body, err := build(tc.request)
				var failure *ProviderError
				if !errors.As(err, &failure) || failure.Kind != ErrorResponse || body != nil {
					t.Fatalf("body=%q error=%v; want a response error without a body", body, err)
				}
			})
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
