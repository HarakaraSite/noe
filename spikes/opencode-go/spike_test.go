package opencodego_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/cli"

	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/OpenRouterTeam/go-sdk/retry"
)

const baseURL = "https://opencode.ai/inference/go/openai/v1"

type runKey struct{}

type wire struct {
	URL           string          `json:"url"`
	UserAgent     string          `json:"userAgent"`
	Session       string          `json:"session"`
	Authenticated bool            `json:"authenticated"`
	Request       json.RawMessage `json:"request"`
	Status        int             `json:"status"`
	ContentType   string          `json:"contentType"`
	Response      string          `json:"response"`
}

// Only selected non-secret headers are recorded. Identity is per request context,
// so a child cannot accidentally reuse its parent's ID through a shared client.
type transport struct {
	client *http.Client
	wires  []*wire
}

func (c *transport) Do(req *http.Request) (*http.Response, error) {
	id, _ := req.Context().Value(runKey{}).(string)
	if id == "" {
		return nil, fmt.Errorf("missing run ID")
	}
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", "noe/"+cli.Version)
	clone.Header.Set("x-opencode-session", id)
	w := &wire{URL: clone.URL.String(), UserAgent: clone.Header.Get("User-Agent"), Session: id, Authenticated: clone.Header.Get("Authorization") != ""}
	if clone.Body != nil {
		body, err := io.ReadAll(clone.Body)
		if err != nil {
			return nil, err
		}
		clone.Body.Close()
		clone.Body = io.NopCloser(bytes.NewReader(body))
		w.Request = append(json.RawMessage(nil), body...)
	}
	c.wires = append(c.wires, w)
	res, err := c.client.Do(clone)
	if res != nil {
		w.Status, w.ContentType = res.StatusCode, res.Header.Get("Content-Type")
		res.Body = &recordBody{ReadCloser: res.Body, wire: w}
	}
	return res, err
}

type recordBody struct {
	io.ReadCloser
	wire *wire
	once sync.Once
	err  error
}

func (b *recordBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	b.wire.Response += string(p[:n])
	return n, e
}
func (b *recordBody) Close() error { b.once.Do(func() { b.err = b.ReadCloser.Close() }); return b.err }

func TestSessionHeaders(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "noe/"+cli.Version {
			t.Error("incorrect User-Agent")
		}
		got = append(got, r.Header.Get("x-opencode-session"))
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	c := &transport{client: server.Client()}
	for _, id := range []string{"parent-run", "child-run", "parent-run"} {
		req, _ := http.NewRequestWithContext(context.WithValue(context.Background(), runKey{}, id), "POST", server.URL, strings.NewReader(`{}`))
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
	if strings.Join(got, ",") != "parent-run,child-run,parent-run" {
		t.Fatalf("session IDs: %v", got)
	}
}

func sdk(t *testing.T, c *transport, key string) *openrouter.OpenRouter {
	t.Helper()
	return openrouter.New(openrouter.WithServerURL(baseURL), openrouter.WithSecurity(key), openrouter.WithClient(c), openrouter.WithRetryConfig(retry.Config{Strategy: "none"}))
}

func raw(v any) json.RawMessage {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return b
}

func TestLive(t *testing.T) {
	keyPath := os.Getenv("NOE_OPENCODE_SPIKE_KEY_FILE")
	if keyPath == "" {
		t.Skip("set NOE_OPENCODE_SPIKE_KEY_FILE to enable paid requests")
	}
	dir := os.Getenv("NOE_OPENCODE_SPIKE_DIR")
	if dir == "" {
		t.Fatal("NOE_OPENCODE_SPIKE_DIR is required")
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(keyBytes))
	if key == "" {
		t.Fatal("empty key")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"chat-sdk", "chat-noe-tool-cycle", "responses-sdk", "responses-sdk-stream"} {
		t.Run(name, func(t *testing.T) {
			started := time.Now()
			ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), runKey{}, filepath.Base(dir)+"-"+name), 75*time.Second)
			defer cancel()
			c := &transport{client: &http.Client{}}
			defer func() {
				data := raw(map[string]any{"case": name, "sdk": "github.com/OpenRouterTeam/go-sdk v0.9.59", "failed": t.Failed(), "durationMs": time.Since(started).Milliseconds(), "wires": c.wires})
				// Redaction is a second guard if an upstream error echoes the token.
				data = bytes.ReplaceAll(data, []byte(key), []byte("[REDACTED]"))
				if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0600); err != nil {
					t.Error(err)
				}
				for _, w := range c.wires {
					if w.UserAgent != "noe/"+cli.Version || w.Session != filepath.Base(dir)+"-"+name {
						t.Error("incorrect request identity")
					}
				}
			}()
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(strings.ReplaceAll(err.Error(), key, "[REDACTED]"))
				}
			}
			switch name {
			case "chat-sdk":
				model := "deepseek-v4.1-flash"
				res, err := sdk(t, c, key).Chat.Send(ctx, components.ChatRequest{Model: &model, MaxTokens: optionalnullable.From(openrouter.Int64(2048)), Messages: []components.ChatMessages{components.CreateChatMessagesUser(components.ChatUserMessage{Content: components.CreateChatUserMessageContentStr("For a JavaScript coding-agent connectivity test, reply exactly NOE_OPENCODE_CHAT_OK.")})}}, nil)
				check(err)
				if res == nil || res.ChatResult == nil || !bytes.Contains(raw(res.ChatResult), []byte("NOE_OPENCODE_CHAT_OK")) {
					t.Fatal("missing expected typed chat response")
				}
			case "chat-noe-tool-cycle":
				p := provider.NewOpenRouter(provider.Options{BaseURL: baseURL, KeyFile: keyPath, HTTPClient: c})
				messages := []json.RawMessage{raw(map[string]any{"role": "user", "content": "You are testing the NOE JavaScript coding agent. Call run_js exactly once with code console.log(6 * 7); to verify the arithmetic. After the tool returns, reply with its numeric output only."})}
				tool := raw(map[string]any{"type": "function", "function": map[string]any{"name": "run_js", "description": "Execute JavaScript with NOE and return stdout, stderr, exitCode and reason.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"code": map[string]any{"type": "string"}}, "required": []string{"code"}}}})
				request := provider.Request{Model: "deepseek-v4.1-flash", Messages: messages, Tools: []json.RawMessage{tool}, CacheKey: filepath.Base(dir) + "-" + name, Effort: "auto", MaxTokens: 4096}
				first, err := p.Generate(ctx, request)
				check(err)
				var message struct {
					ToolCalls []struct {
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				}
				check(json.Unmarshal(first.Message, &message))
				if first.FinishReason != "tool_calls" || len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != "run_js" {
					t.Fatalf("expected run_js: %s", first.Message)
				}
				call := message.ToolCalls[0]
				var args struct {
					Code string `json:"code"`
				}
				check(json.Unmarshal([]byte(call.Function.Arguments), &args))
				binary, err := filepath.Abs("../../noe")
				check(err)
				cmd := exec.CommandContext(ctx, binary, "eval", args.Code)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				out, err := cmd.Output()
				check(err)
				if strings.TrimSpace(string(out)) != "42" {
					t.Fatalf("JS stdout: %q", out)
				}
				request.Messages = append(messages, first.Message, raw(map[string]any{"role": "tool", "tool_call_id": call.ID, "content": string(raw(map[string]any{"stdout": string(out), "stderr": stderr.String(), "exitCode": 0, "reason": "completed"}))}))
				last, err := p.Generate(ctx, request)
				check(err)
				if last.FinishReason != "stop" || !bytes.Contains(last.Message, []byte("42")) {
					t.Fatalf("missing final answer: %s", last.Message)
				}
				check(os.WriteFile(filepath.Join(dir, "chat-noe-tool-result.json"), raw(map[string]any{"code": args.Code, "stdout": string(out), "first": first, "last": last}), 0600))
			case "responses-sdk", "responses-sdk-stream":
				request := components.ResponsesRequest{
					Model:           openrouter.String("gpt-6-luna"),
					Input:           openrouter.Pointer(components.CreateInputsUnionStr("For a JavaScript coding-agent connectivity test, reply exactly NOE_OPENCODE_RESPONSES_OK.")),
					MaxOutputTokens: optionalnullable.From(openrouter.Int64(2048)),
					Stream:          openrouter.Bool(name == "responses-sdk-stream"),
					// v0.9.59 fails to encode this field's default when omitted.
					ServiceTier: optionalnullable.From(openrouter.Pointer(components.ResponsesRequestServiceTierAuto)),
				}
				res, err := sdk(t, c, key).Responses.Send(ctx, request, nil)
				check(err)
				if name == "responses-sdk" {
					if res == nil || res.OpenResponsesResult == nil || !bytes.Contains(raw(res.OpenResponsesResult), []byte("NOE_OPENCODE_RESPONSES_OK")) {
						t.Fatal("missing expected typed Responses result")
					}
				} else {
					if res == nil || res.EventStream == nil {
						t.Fatal("missing Responses event stream")
					}
					defer res.EventStream.Close()
					var events []json.RawMessage
					for res.EventStream.Next() {
						events = append(events, raw(res.EventStream.Value()))
					}
					check(res.EventStream.Err())
					check(os.WriteFile(filepath.Join(dir, "responses-events.json"), raw(events), 0600))
					if !bytes.Contains(raw(events), []byte("NOE_OPENCODE_RESPONSES_OK")) || !bytes.Contains(raw(events), []byte("response.completed")) {
						t.Fatal("missing streamed output or completed event")
					}
				}
			}
		})
	}
}
