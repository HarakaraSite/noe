package agent

import (
	"context"
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

func BenchmarkManagedContextPrepare(b *testing.B) {
	for _, tc := range []struct {
		name                   string
		messages, contentBytes int
	}{
		{"short_16KiB", 4, 4 << 10},
		{"long_1MiB", 256, 4 << 10},
		{"large_messages_1MiB", 2, 512 << 10},
	} {
		b.Run(tc.name, func(b *testing.B) {
			history := make([]jsonv1.RawMessage, tc.messages)
			for i := range history {
				history[i], _ = json.Marshal(struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{"assistant", strings.Repeat("x", tc.contentBytes)})
			}
			tools := []jsonv1.RawMessage{jsonv1.RawMessage(`{"type":"function","function":{"name":"run_js","parameters":{"type":"object"}}}`)}

			h := NewHost(Options{})
			b.Cleanup(h.Close)
			op := h.NewOperation(context.Background())
			b.Cleanup(op.Close)
			runContext, cancelRun := context.WithCancel(context.Background())
			b.Cleanup(cancelRun)
			conversation, err := initialConversation("fixed system", "original instruction")
			if err != nil {
				b.Fatal(err)
			}
			for _, message := range history {
				if err := conversation.appendRaw("block", []jsonv1.RawMessage{message}); err != nil {
					b.Fatal(err)
				}
			}
			instanceDone := make(chan struct{})
			close(instanceDone)
			instance := &agentInstance{
				host: h, id: "managed-context-benchmark", state: "running", conversation: conversation,
				ctx: context.Background(), done: instanceDone,
			}
			const stepID = "managed-context-benchmark-step"
			run := &agentRun{
				id: "managed-context-benchmark-run", operation: op, ctx: runContext, cancel: cancelRun,
				config:   runConfig{Model: "benchmark/model", MaxTokens: 128, Effort: "auto"},
				instance: instance, modelStep: &modelStepState{id: stepID},
			}
			run.context.resetStep(stepID)
			run.context.policyDisabled = true
			h.mu.Lock()
			h.runs[run.id] = run
			h.instances[instance.id] = instance
			h.mu.Unlock()
			request := contextPrepareRequest{
				RunID: run.id, ModelStepID: stepID, HistoryRevision: conversation.Revision,
				Tools: tools, Final: true,
			}
			prepare := func() {
				if _, err := h.contextPrepare(op, request); err != nil {
					b.Fatal(err)
				}
			}
			prepare() // Keep setup and cache population outside the timed loop.
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				prepare()
			}
		})
	}
}
