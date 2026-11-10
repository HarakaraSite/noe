package provider

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"strings"
	"testing"
)

func BenchmarkResponsesRequestBody(b *testing.B) {
	for _, size := range []struct {
		name     string
		messages int
		bytes    int
	}{
		{"4x4KiB", 4, 4 << 10},
		{"256x4KiB", 256, 4 << 10},
		{"2x512KiB", 2, 512 << 10},
	} {
		b.Run(size.name, func(b *testing.B) {
			message, err := json.Marshal(map[string]string{"role": "user", "content": strings.Repeat("x", size.bytes)})
			if err != nil {
				b.Fatal(err)
			}
			request := Request{
				Model: "fixture/model", Messages: make([]jsonv1.RawMessage, size.messages),
				Tools:    []jsonv1.RawMessage{jsonv1.RawMessage(`{"type":"function","function":{"name":"run_js","parameters":{"type":"object"}}}`)},
				CacheKey: "session", Effort: "high",
			}
			for index := range request.Messages {
				request.Messages[index] = message
			}
			profile := Definition{SessionField: "prompt_cache_key", EffortField: "reasoning.effort"}
			b.ReportAllocs()
			b.SetBytes(int64(size.messages * size.bytes))
			for b.Loop() {
				if _, err := configuredResponsesRequestBody(profile, request); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
