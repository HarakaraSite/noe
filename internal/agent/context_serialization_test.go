package agent

import (
	"crypto/sha256"
	"encoding/hex"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"reflect"
	"strings"
	"testing"
)

func TestContextHistorySerializationMatchesNativeJSON(t *testing.T) {
	manyMessages := make([]jsonv1.RawMessage, 256)
	for i := range manyMessages {
		manyMessages[i] = jsonv1.RawMessage(`{"role":"assistant","content":"` + strings.Repeat("x", 4096) + `"}`)
	}
	for _, tc := range []struct {
		name            string
		messages, tools []jsonv1.RawMessage
	}{
		{name: "nil collections"},
		{name: "empty collections", messages: []jsonv1.RawMessage{}, tools: []jsonv1.RawMessage{}},
		{
			name: "whitespace escapes and unicode",
			messages: []jsonv1.RawMessage{
				jsonv1.RawMessage(` { "role": "user", "content": "東京😀 <tag>& \"quote\"\n\\\u2028", "number": 1.00 } `),
				jsonv1.RawMessage(`{"role":"assistant","content":null,"opaque":{}}`),
			},
			tools: []jsonv1.RawMessage{jsonv1.RawMessage(` { "type" : "function", "parameters": {"properties":{},"required":[]} } `)},
		},
		{
			name:     "large value",
			messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"assistant","content":"` + strings.Repeat("x", 1<<20) + `"}`)},
		},
		{
			name:     "many messages",
			messages: manyMessages,
		},
		{
			name:  "large tool",
			tools: []jsonv1.RawMessage{jsonv1.RawMessage(`{"name":"run_js","description":"` + strings.Repeat("x", 1<<20) + `"}`)},
		},
		{
			name:  "tools without messages",
			tools: []jsonv1.RawMessage{jsonv1.RawMessage(`{"name":"run_js"}`)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			messages, err := json.Marshal(tc.messages)
			if err != nil {
				t.Fatal(err)
			}
			tools, err := json.Marshal(tc.tools)
			if err != nil {
				t.Fatal(err)
			}
			stats, err := measureContextHistory(tc.messages, tc.tools, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := contextHistoryStats{messagesBytes: len(messages), messageCount: len(tc.messages), toolsBytes: len(tools)}
			if stats != want {
				t.Fatalf("context measurement = %+v, want %+v", stats, want)
			}
			checkContextDigest(t, tc.messages, tc.tools)
		})
	}
}

func TestContextHistorySerializationRejectsInvalidJSON(t *testing.T) {
	for _, raw := range []jsonv1.RawMessage{
		jsonv1.RawMessage(`{"content":`),
		jsonv1.RawMessage(`{"content":"first","content":"second"}`),
		jsonv1.RawMessage("{\"content\":\"\xff\"}"),
	} {
		for _, inTools := range []bool{false, true} {
			messages, tools := []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"` + strings.Repeat("prefix", 2048) + `"}`), raw}, []jsonv1.RawMessage(nil)
			if inTools {
				messages, tools = messages[:1], []jsonv1.RawMessage{raw}
			}
			if _, err := measureContextHistory(messages, tools, nil); err == nil {
				t.Fatalf("invalid JSON accepted (tools=%v): %q", inTools, raw)
			}
			checkContextDigest(t, messages, tools)
		}
	}
}

func checkContextDigest(t *testing.T, messages, tools []jsonv1.RawMessage) {
	t.Helper()
	encoded, _ := json.Marshal(struct {
		Messages []jsonv1.RawMessage
		Tools    []jsonv1.RawMessage
	}{messages, tools})
	want := sha256.Sum256(encoded)
	if got := contextHistoryDigest(messages, tools); got != hex.EncodeToString(want[:]) {
		t.Fatalf("context digest = %s, want %x", got, want)
	}
}

func TestContextHistoryPartsKeepSerializedWeights(t *testing.T) {
	messages := []jsonv1.RawMessage{
		jsonv1.RawMessage(` {"role":"system","content":"policy <>&"} `),
		jsonv1.RawMessage(`{"role":"assistant","tool_calls":[{"id":"call"}]}`),
		jsonv1.RawMessage(`{"role":"tool","tool_call_id":"call","content":"東京😀"}`),
		jsonv1.RawMessage(`{"role":"user","content":[{"type":"noe_image","imageId":"image-1"}]}`),
		jsonv1.RawMessage(`{"role":"assistant","content":"last block"}`),
	}
	tools := []jsonv1.RawMessage{jsonv1.RawMessage(`{"name":"run_js"}`)}
	layout := &contextHistoryLayout{Revision: 1, Entries: []contextHistoryEntry{
		{ID: "policy", Kind: "protected", Count: 1},
		{ID: "tool-exchange", Kind: "block", Count: 2},
		{ID: "image", Kind: "protected", Count: 1},
		{ID: "answer", Kind: "block", Count: 1},
	}}
	estimates := map[string]int64{"image-1": 123}
	stats, err := measureContextHistory(messages, tools, estimates)
	if err != nil {
		t.Fatal(err)
	}
	fixed, execution, weights, err := historyPartsFromStats(messages, layout, estimates, stats)
	if err != nil {
		t.Fatal(err)
	}
	protected, _ := json.Marshal([]jsonv1.RawMessage{messages[0], messages[3]})
	wantFixed := estimateContextTokens(len(protected), stats.toolsBytes) + 123
	wantExecution := max(0, estimateContextTokens(stats.messagesBytes, stats.toolsBytes)+123-wantFixed)
	var wantWeights []int64
	for _, group := range [][]jsonv1.RawMessage{messages[1:3], messages[4:]} {
		encoded, _ := json.Marshal(group)
		wantWeights = append(wantWeights, estimateContextTokens(len(encoded)-1, 0))
	}
	if fixed != wantFixed || execution != wantExecution || !reflect.DeepEqual(weights, wantWeights) {
		t.Fatalf("parts = %d/%d/%v, want %d/%d/%v", fixed, execution, weights, wantFixed, wantExecution, wantWeights)
	}
}
