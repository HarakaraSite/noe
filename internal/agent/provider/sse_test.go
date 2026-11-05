package provider

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"errors"
	"io"
	"strings"
	"testing"
)

type sseReadChunks struct {
	source *strings.Reader
	size   int
}

func (r *sseReadChunks) Read(p []byte) (int, error) {
	if len(p) > r.size {
		p = p[:r.size]
	}
	return r.source.Read(p)
}

func TestObservedBodyAssemblesChatAcrossReadBoundaries(t *testing.T) {
	wire := "\xef\xbb\xbf: heartbeat\r\nevent: message\r\ndata: {\"choices\":[\r\ndata: {\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"東京\"}}]}\r\n\r\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"😀\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\r\n\r\ndata: [DONE]\r\n\r\n"
	for _, size := range []int{1, 7, 4096} {
		body := newObservedBody(io.NopCloser(&sseReadChunks{source: strings.NewReader(wire), size: size}))
		var observed strings.Builder
		body.observe = func(event StreamEvent) {
			body.snapshot() // Observers run without the parser lock held.
			if event.Type == "message_delta" {
				observed.WriteString(event.Text)
			}
		}
		if _, err := io.Copy(io.Discard, body); err != nil {
			t.Fatal(err)
		}
		done, readErr, consumeErr := body.snapshot()
		if !done || readErr != nil || consumeErr != nil {
			t.Fatalf("chunk=%d done=%t read=%v consume=%v", size, done, readErr, consumeErr)
		}
		response, err := body.response()
		if err != nil {
			t.Fatal(err)
		}
		var message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(response.Message, &message); err != nil {
			t.Fatal(err)
		}
		if message.Role != "assistant" || message.Content != "東京😀" || observed.String() != message.Content || response.FinishReason != "stop" || !strings.Contains(string(response.Usage), `"prompt_tokens":4`) {
			t.Fatalf("response=%+v observed=%q", response, observed.String())
		}
		for _, line := range body.dataLines {
			if line != "" {
				t.Fatal("completed frame retained its raw data line")
			}
		}
	}
}

func TestObservedBodyChatAccumulatesBeforeStreamCompletion(t *testing.T) {
	body := newObservedBody(nil)
	frame := []byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"fragment\"}}]}\n\n")
	for range 2048 {
		body.feedLocked(frame)
	}
	if body.chat.content.Len() != 2048*len("fragment") {
		t.Fatal("deltas were not consumed while streaming")
	}
	body.feedLocked([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	response, err := body.response()
	if err != nil {
		t.Fatal(err)
	}
	var message struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(response.Message, &message); err != nil {
		t.Fatal(err)
	}
	if message.Content != strings.Repeat("fragment", 2048) {
		t.Fatal("assembled content changed")
	}
}

func feedNativeEvent(body *observedBody, event string) {
	body.feedLocked([]byte("data: " + event + "\n\n"))
}

func TestObservedBodyResponsesKeepsFinalItemsInsteadOfDeltas(t *testing.T) {
	body := newObservedBody(nil)
	body.chat = nil
	body.native = &responsesAccumulator{}
	for range 2048 {
		feedNativeEvent(body, `{"type":"response.output_text.delta","delta":"fragment"}`)
	}
	if len(body.native.items) != 0 || body.native.envelope != nil {
		t.Fatal("text deltas retained as final output")
	}
	feedNativeEvent(body, `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","content":[{"type":"output_text","text":"stale"}]}}`)
	feedNativeEvent(body, `{"type":"response.output_item.done","output_index":2,"item":{"type":"reasoning","encrypted_content":"old"}}`)
	feedNativeEvent(body, `{"type":"response.output_item.done","output_index":2,"item":{"type":"reasoning","encrypted_content":"keep","opaque":{"signature":"signed"}}}`)
	feedNativeEvent(body, `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"final"}]}],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`)
	feedNativeEvent(body, `{"malformed":`)
	if _, _, err := body.snapshot(); err != nil {
		t.Fatalf("events after terminal completion changed the result: %v", err)
	}
	if _, exists := body.native.envelope["output"]; exists {
		t.Fatal("terminal envelope retained a duplicate raw output array")
	}
	for range 2 {
		response, err := body.response()
		if err != nil {
			t.Fatal(err)
		}
		var message struct {
			Content string              `json:"content"`
			Items   []jsonv1.RawMessage `json:"_noe_response_items"`
		}
		if err := json.Unmarshal(response.Message, &message); err != nil {
			t.Fatal(err)
		}
		if message.Content != "final" || len(message.Items) != 2 || !strings.Contains(string(message.Items[1]), `"encrypted_content":"keep"`) || !strings.Contains(string(message.Items[1]), `"signature":"signed"`) {
			t.Fatalf("final items lost: %s", response.Message)
		}
		if !strings.Contains(string(response.Usage), `"total_tokens":6`) {
			t.Fatal("usage lost")
		}
	}
}

func TestObservedBodyPreservesReadErrorsAndIncompleteStreams(t *testing.T) {
	cause := errors.New("read failed")
	body := newObservedBody(&sseErrorReader{err: cause})
	if _, err := body.Read(make([]byte, 32)); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	done, readErr, consumeErr := body.snapshot()
	if done || !errors.Is(readErr, cause) || consumeErr != nil {
		t.Fatalf("done=%t read=%v consume=%v", done, readErr, consumeErr)
	}
	body = newObservedBody(nil)
	body.chat = nil
	body.native = &responsesAccumulator{}
	feedNativeEvent(body, `{"type":"response.output_text.delta","delta":"unfinished"}`)
	if _, err := body.response(); err == nil {
		t.Fatal("incomplete response accepted")
	}
}

type sseErrorReader struct{ err error }

func (r *sseErrorReader) Read([]byte) (int, error) { return 0, r.err }
func (r *sseErrorReader) Close() error             { return nil }

func TestResponsesQuotaSurvivesEarlierAssemblyFailure(t *testing.T) {
	for _, code := range []string{"subscription_sharing_usage_limit_exceeded", "subscription_sharing_usage_unavailable"} {
		body := newObservedBody(nil)
		body.chat = nil
		body.native = &responsesAccumulator{}
		feedNativeEvent(body, `{"type":"response.output_item.done","output_index":0,"item":null}`)
		feedNativeEvent(body, `{"type":"response.failed","response":{"error":{"code":"`+code+`"}}}`)
		if !body.nativeQuota() {
			t.Fatal("quota classification lost after assembly error")
		}
		if _, err := body.response(); err == nil {
			t.Fatal("assembly failure lost")
		}
		err := nativeStreamFailure(&callState{body: body})
		var failure *ProviderError
		if !errors.As(err, &failure) || failure.Kind != ErrorQuota {
			t.Fatalf("quota failure=%v", err)
		}
	}
}

func TestResponsesQuotaClassificationIgnoresUnrelatedFieldErrors(t *testing.T) {
	body := newObservedBody(nil)
	body.chat = nil
	body.native = &responsesAccumulator{}
	feedNativeEvent(body, `{"type":"response.failed","output_index":"invalid","response":{"error":{"code":"subscription_sharing_usage_limit_exceeded"}}}`)
	if !body.nativeQuota() {
		t.Fatal("quota code lost because of an unrelated malformed field")
	}
	if _, err := body.response(); err == nil {
		t.Fatal("malformed event accepted")
	}
}

func TestResponsesTerminalNullReturnsError(t *testing.T) {
	body := newObservedBody(nil)
	body.chat = nil
	body.native = &responsesAccumulator{}
	feedNativeEvent(body, `{"type":"response.completed","response":null}`)
	if _, err := body.response(); err == nil {
		t.Fatal("null completion accepted")
	}
}

func BenchmarkObservedBodyChatAssembly(b *testing.B) {
	frame := "data: " + `{"id":"response-probe","created":1,"model":"dummy-model","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"` + strings.Repeat("x", 32) + `"}}]}` + "\n\n"
	wire := strings.Repeat(frame, 16384) + "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	b.ReportAllocs()
	for b.Loop() {
		body := newObservedBody(io.NopCloser(strings.NewReader(wire)))
		if _, err := io.Copy(io.Discard, body); err != nil {
			b.Fatal(err)
		}
		if _, err := body.response(); err != nil {
			b.Fatal(err)
		}
	}
}
