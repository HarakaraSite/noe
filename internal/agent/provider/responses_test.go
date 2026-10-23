package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func nativeResponseFixture(output string) string {
	return `{"id":"response-1","object":"response","created_at":1,"status":"completed","model":"fixture/model","error":null,"incomplete_details":null,"parallel_tool_calls":true,"tool_choice":"auto","tools":[],"output":` + output + `,"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`
}

func TestConfiguredResponsesCancellationClosesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": waiting\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	key := filepath.Join(t.TempDir(), "key")
	os.WriteFile(key, []byte("fixture"), 0600)
	client := &countingHTTPClient{base: server.Client(), readStarted: make(chan struct{})}
	p := NewOpenRouter(Options{KeyFile: key, BaseURL: server.URL, HTTPClient: client, Profile: &Definition{API: "responses", Path: "/responses"}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := p.Generate(ctx, Request{Model: "model", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"wait"}`)}})
		done <- err
	}()
	select {
	case <-client.readStarted:
	case <-ctx.Done():
		t.Fatal("stream did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: %v", err)
	}
	if client.closes.Load() != 1 || client.calls.Load() != 1 {
		t.Fatalf("calls=%d closes=%d", client.calls.Load(), client.closes.Load())
	}
}

func TestConfiguredResponsesToolCycleKeepsNativeReasoning(t *testing.T) {
	var calls atomic.Int32
	firstOutput := `[{"id":"rs-1","type":"reasoning","summary":[{"type":"summary_text","text":"check"}],"encrypted_content":"opaque-reasoning"},{"id":"fc-1","type":"function_call","status":"completed","call_id":"call-1","name":"run_js","arguments":"{\"code\":\"console.log(42)\"}"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := calls.Add(1)
		if r.URL.Path != "/custom/responses" || r.Header.Get("x-opencode-session") != "session-1" || r.Header.Get("User-Agent") != "noe/test" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("incorrect endpoint/identity")
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if string(body["store"]) != "false" || string(body["reasoning"]) != `{"effort":"high"}` {
			t.Errorf("request settings: %v", body)
		}
		if !strings.Contains(string(body["include"]), "reasoning.encrypted_content") {
			t.Error("stateless reasoning content was not requested")
		}
		if index == 2 && (!strings.Contains(string(body["input"]), "opaque-reasoning") || !strings.Contains(string(body["input"]), "function_call_output")) {
			t.Errorf("native reasoning or tool output lost: %s", body["input"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		output := firstOutput
		if index == 1 {
			writeSSE(w, `{"type":"response.reasoning_summary_text.delta","item_id":"rs-1","output_index":0,"summary_index":0,"delta":"check","sequence_number":1}`)
		} else {
			output = `[{"id":"msg-1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"42","annotations":[]}]}]`
			writeSSE(w, `{"type":"response.output_text.delta","item_id":"msg-1","output_index":0,"content_index":0,"delta":"42","sequence_number":1}`)
		}
		writeSSE(w, `{"type":"response.completed","sequence_number":2,"response":`+nativeResponseFixture(output)+`}`)
		// A terminal frame is enough; Generate must not wait for HTTP EOF.
		<-r.Context().Done()
	}))
	defer server.Close()
	key := filepath.Join(t.TempDir(), "key")
	os.WriteFile(key, []byte("fixture"), 0600)
	p := NewOpenRouter(Options{KeyFile: key, BaseURL: server.URL, Profile: &Definition{API: "responses", Path: "/custom/responses", SessionHeader: "x-opencode-session", EffortField: "reasoning.effort", Headers: map[string]string{"User-Agent": "noe/test"}}})
	var observations []StreamEvent
	request := Request{RunID: "run-1", SessionID: "session-1", Model: "fixture/model", Effort: "high", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"calculate"}`)}, Tools: []json.RawMessage{json.RawMessage(`{"type":"function","function":{"name":"run_js","parameters":{"type":"object"}}}`)}, Observe: func(e StreamEvent) { observations = append(observations, e) }}
	first, err := p.Generate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.FinishReason != "tool_calls" || !strings.Contains(string(first.Message), "opaque-reasoning") {
		t.Fatalf("first result: %s", first.Message)
	}
	request.Messages = append(request.Messages, first.Message, json.RawMessage(`{"role":"tool","tool_call_id":"call-1","content":"42"}`))
	request.RunID = "run-2"
	last, err := p.Generate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if last.FinishReason != "stop" || !strings.Contains(string(last.Message), "42") || calls.Load() != 2 || len(observations) != 2 || observations[0].Type != "thinking_summary" || observations[1].Type != "message_delta" {
		t.Fatalf("result=%s observations=%v calls=%d", last.Message, observations, calls.Load())
	}
}

func TestResponsesJavaScriptCustomToolRoundTripWithMixedCalls(t *testing.T) {
	profile := Definition{API: "responses", RunJSInput: "javascript"}
	request := Request{
		Model:    "fixture/model",
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"calculate"}`)},
		Tools: []json.RawMessage{
			json.RawMessage(`{"type":"function","function":{"name":"run_js","description":"Run JavaScript","parameters":{"type":"object","properties":{"code":{"type":"string"}}}}}`),
			json.RawMessage(`{"type":"function","function":{"name":"lookup","description":"Look up a value","parameters":{"type":"object"}}}`),
		},
	}
	body, err := configuredResponsesRequestBody(profile, request)
	if err != nil {
		t.Fatal(err)
	}
	var outgoing struct {
		Tools []map[string]json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &outgoing); err != nil {
		t.Fatal(err)
	}
	if len(outgoing.Tools) != 2 || string(outgoing.Tools[0]["type"]) != `"custom"` || string(outgoing.Tools[0]["name"]) != `"run_js"` || string(outgoing.Tools[0]["description"]) != `"Run JavaScript"` || string(outgoing.Tools[0]["format"]) != `{"type":"text"}` || outgoing.Tools[0]["parameters"] != nil {
		t.Fatalf("run_js was not sent as a text custom tool: %s", body)
	}
	if string(outgoing.Tools[1]["type"]) != `"function"` || string(outgoing.Tools[1]["name"]) != `"lookup"` || outgoing.Tools[1]["parameters"] == nil {
		t.Fatalf("unrelated function tool changed: %s", body)
	}

	rawInput := "console.log(\"x\")\nconst result = 42;"
	output := `[{"id":"rs-1","type":"reasoning","summary":[],"encrypted_content":"opaque-reasoning","provider_extra":{"keep":true}},{"id":"ct-1","type":"custom_tool_call","status":"completed","call_id":"custom-1","name":"run_js","input":` + mustJSON(rawInput) + `,"provider_extra":{"keep":true}},{"id":"fc-1","type":"function_call","status":"completed","call_id":"function-1","name":"lookup","arguments":"{\"query\":\"x\"}","provider_extra":17}]`
	response, err := responseFromNative(json.RawMessage(nativeResponseFixture(output)))
	if err != nil {
		t.Fatal(err)
	}
	var assistant struct {
		ToolCalls []struct {
			ID       string `json:"id"`
			RawInput string `json:"_noe_raw_input"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal(response.Message, &assistant); err != nil {
		t.Fatal(err)
	}
	if len(assistant.ToolCalls) != 2 {
		t.Fatalf("normalized calls: %s", response.Message)
	}
	var customArguments struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(assistant.ToolCalls[0].Function.Arguments), &customArguments); err != nil {
		t.Fatal(err)
	}
	if assistant.ToolCalls[0].ID != "custom-1" || assistant.ToolCalls[0].Function.Name != "run_js" || assistant.ToolCalls[0].RawInput != rawInput || customArguments.Code != rawInput {
		t.Fatalf("custom call normalization: %+v arguments=%+v", assistant.ToolCalls[0], customArguments)
	}
	if assistant.ToolCalls[1].ID != "function-1" || assistant.ToolCalls[1].Function.Name != "lookup" || assistant.ToolCalls[1].RawInput != "" || assistant.ToolCalls[1].Function.Arguments != `{"query":"x"}` {
		t.Fatalf("function call changed: %+v", assistant.ToolCalls[1])
	}

	request.Messages = append(request.Messages, response.Message,
		json.RawMessage(`{"role":"tool","tool_call_id":"custom-1","content":"custom result"}`),
		json.RawMessage(`{"role":"tool","tool_call_id":"function-1","content":"function result"}`),
	)
	body, err = configuredResponsesRequestBody(profile, request)
	if err != nil {
		t.Fatal(err)
	}
	var continuation struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(body, &continuation); err != nil {
		t.Fatal(err)
	}
	if len(continuation.Input) != 6 {
		t.Fatalf("continuation input has %d items: %s", len(continuation.Input), body)
	}
	var nativeCustom struct {
		Input string `json:"input"`
	}
	if json.Unmarshal(continuation.Input[2], &nativeCustom) != nil || nativeCustom.Input != rawInput || !strings.Contains(string(continuation.Input[1]), "opaque-reasoning") || !strings.Contains(string(continuation.Input[1]), `"provider_extra":{"keep":true}`) {
		t.Fatalf("native output items were not retained: %s", body)
	}
	var customOutput, functionOutput struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(continuation.Input[4], &customOutput); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(continuation.Input[5], &functionOutput); err != nil {
		t.Fatal(err)
	}
	if customOutput.Type != "custom_tool_call_output" || customOutput.CallID != "custom-1" || customOutput.Output != "custom result" {
		t.Errorf("custom output did not match its call: %+v", customOutput)
	}
	if functionOutput.Type != "function_call_output" || functionOutput.CallID != "function-1" || functionOutput.Output != "function result" {
		t.Errorf("function output did not match its call: %+v", functionOutput)
	}

	// An omitted setting remains the existing JSON function-tool contract.
	legacyBody, err := configuredResponsesRequestBody(Definition{API: "responses"}, Request{
		Model: "fixture/model", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"calculate"}`)}, Tools: request.Tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Tools []map[string]json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(legacyBody, &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy.Tools) != 2 || string(legacy.Tools[0]["type"]) != `"function"` || string(legacy.Tools[0]["name"]) != `"run_js"` || legacy.Tools[0]["parameters"] == nil {
		t.Fatalf("default JSON tool contract changed: %s", legacyBody)
	}
}

func TestConfiguredChatReasoningContentIsConcatenatedAndReplayed(t *testing.T) {
	for _, field := range []string{"reasoning_content", "custom_thinking"} {
		t.Run(field, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, text := range []string{"first ", "second", ""} {
					writeSSE(w, fmt.Sprintf(`{"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant",%q:%q},"finish_reason":null}]}`, field, text))
				}
				writeSSE(w, `{"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`)
				writeSSE(w, "[DONE]")
			}))
			defer server.Close()
			key := filepath.Join(t.TempDir(), "key")
			os.WriteFile(key, []byte("fixture"), 0600)
			p := NewOpenRouter(Options{KeyFile: key, BaseURL: server.URL, Profile: &Definition{API: "chat-completions", Path: "/chat/completions", ReasoningField: field}})
			var thinking strings.Builder
			result, err := p.Generate(context.Background(), Request{Model: "m", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"task"}`)}, Observe: func(e StreamEvent) {
				if e.Type == "thinking" {
					thinking.WriteString(e.Text)
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			var message map[string]json.RawMessage
			json.Unmarshal(result.Message, &message)
			if string(message[field]) != `"first second"` || thinking.String() != "first second" {
				t.Fatalf("reasoning lost: %s, observed=%q", result.Message, thinking.String())
			}
		})
	}
}

func TestResponsesImagePartsContainOnlyNativeFields(t *testing.T) {
	raw := json.RawMessage(`{"role":"user","content":[{"type":"text","text":"inspect"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA","detail":"low"}}]}`)
	body, err := configuredResponsesRequestBody(Definition{}, Request{Model: "fixture/model", Messages: []json.RawMessage{raw}, Effort: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Input []struct {
			Content []map[string]json.RawMessage `json:"content"`
		} `json:"input"`
	}
	if json.Unmarshal(body, &request) != nil || len(request.Input) != 1 || len(request.Input[0].Content) != 2 {
		t.Fatalf("body=%s", body)
	}
	part := request.Input[0].Content[1]
	if len(part) != 3 || string(part["type"]) != `"input_image"` || string(part["image_url"]) != `"data:image/png;base64,AAAA"` || string(part["detail"]) != `"low"` {
		t.Fatalf("native image part=%s", body)
	}
	if string(request.Input[0].Content[0]["type"]) != `"input_text"` {
		t.Fatalf("native text part=%s", body)
	}
	if string(raw) != `{"role":"user","content":[{"type":"text","text":"inspect"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA","detail":"low"}}]}` {
		t.Fatal("mutated history")
	}
}

func TestResponsesWithoutToolsSendsEmptyArrayThroughSDK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if string(request["tools"]) != "[]" {
			t.Errorf("tool-less Responses tools=%s", request["tools"])
			http.Error(w, "tools must be an array", 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"type":"response.completed","sequence_number":1,"response":`+nativeResponseFixture(`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"standalone","annotations":[]}]}]`)+`}`)
	}))
	defer server.Close()
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	provider := NewOpenRouter(Options{KeyFile: key, BaseURL: server.URL, Profile: &Definition{API: "responses", Path: "/responses"}})
	response, err := provider.Generate(context.Background(), Request{Model: "fixture/model", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"standalone"}`)}})
	if err != nil || response.FinishReason != "stop" || !strings.Contains(string(response.Message), "standalone") {
		t.Fatalf("response=%+v error=%v", response, err)
	}
}
