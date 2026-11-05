package agent

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"fmt"
	"strings"
	"testing"
)

func BenchmarkContextHistory(b *testing.B) {
	for _, tc := range []struct {
		name                   string
		messages, contentBytes int
	}{
		{"short_16KiB", 4, 4 << 10},
		{"long_1MiB", 256, 4 << 10},
		{"large_messages_1MiB", 2, 512 << 10},
	} {
		b.Run(tc.name, func(b *testing.B) {
			messages := make([]jsonv1.RawMessage, tc.messages)
			layout := &contextHistoryLayout{Revision: 1}
			for i := range messages {
				role, kind := "assistant", "block"
				if i < 2 {
					role, kind = "user", "protected"
				}
				var err error
				messages[i], err = json.Marshal(struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{role, strings.Repeat("x", tc.contentBytes)})
				if err != nil {
					b.Fatal(err)
				}
				layout.Entries = append(layout.Entries, contextHistoryEntry{ID: fmt.Sprint(i), Kind: kind, Count: 1})
			}
			tools := []jsonv1.RawMessage{jsonv1.RawMessage(`{"type":"function","function":{"name":"run_js","parameters":{"type":"object"}}}`)}
			b.Run("measure", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := measureContextHistory(messages, tools, nil); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("digest", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					_ = contextHistoryDigest(messages, tools)
				}
			})
			b.Run("prepare", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					stats, err := measureContextHistory(messages, tools, nil)
					if err != nil {
						b.Fatal(err)
					}
					if _, _, _, err := historyPartsFromStats(messages, layout, nil, stats); err != nil {
						b.Fatal(err)
					}
					_ = contextHistoryDigest(messages, tools)
				}
			})
		})
	}
}
