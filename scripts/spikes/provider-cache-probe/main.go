// provider-cache-probe exercises provider SDKs without an agent/task runtime.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/models/operations"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/OpenRouterTeam/go-sdk/retry"
)

type record struct {
	Attempt       int             `json:"attempt"`
	URL           string          `json:"url"`
	BodyHash      string          `json:"bodyHash"`
	SessionHeader string          `json:"sessionHeader,omitempty"`
	BodySession   string          `json:"bodySession,omitempty"`
	UserAgent     string          `json:"userAgent"`
	HTTPStatus    int             `json:"httpStatus"`
	ResponseModel string          `json:"responseModel,omitempty"`
	ReturnedKey   string          `json:"returnedCacheKey,omitempty"`
	RawUsage      json.RawMessage `json:"rawUsage,omitempty"`
	APIError      *apiError       `json:"apiError,omitempty"`
}

type apiError struct {
	Code    json.RawMessage `json:"code,omitempty"`
	Param   string          `json:"param,omitempty"`
	Message string          `json:"message,omitempty"`
}

type trial struct {
	Name            string            `json:"name"`
	Provider        string            `json:"provider"`
	Model           string            `json:"model"`
	API             string            `json:"api"`
	SDK             string            `json:"sdk"`
	RequestEncoding string            `json:"requestEncoding"`
	CredentialFile  string            `json:"credentialFile"`
	SessionID       string            `json:"sessionId"`
	Requests        []*record         `json:"requests"`
	ParsedUsage     []json.RawMessage `json:"parsedUsage"`
	Error           string            `json:"error,omitempty"`
}

// Observe actual SDK HTTP requests and forward the original body unchanged.
type recorder struct {
	trial  *trial
	header string
}

func (c *recorder) Do(req *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(req.URL.Path, "/responses") && !strings.HasSuffix(req.URL.Path, "/chat/completions") {
		return http.DefaultClient.Do(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	digest := sha256.Sum256(body)
	r := &record{Attempt: len(c.trial.Requests) + 1, URL: req.URL.String(), BodyHash: hex.EncodeToString(digest[:]), UserAgent: req.Header.Get("User-Agent")}
	if c.header != "" {
		r.SessionHeader = req.Header.Get(c.header)
	}
	for _, key := range []string{"session_id", "prompt_cache_key"} {
		if raw := fields[key]; raw != nil {
			_ = json.Unmarshal(raw, &r.BodySession)
		}
	}
	c.trial.Requests = append(c.trial.Requests, r)
	response, err := http.DefaultClient.Do(req)
	if response != nil {
		r.HTTPStatus = response.StatusCode
		if response.StatusCode >= 400 {
			data, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			var failed struct {
				Error apiError `json:"error"`
			}
			if json.Unmarshal(data, &failed) == nil {
				secret := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
				if secret != "" {
					failed.Error.Message = strings.ReplaceAll(failed.Error.Message, secret, "[redacted]")
				}
				r.APIError = &failed.Error
			}
			response.Body = io.NopCloser(bytes.NewReader(data))
		}
		response.Body = &usageBody{ReadCloser: response.Body, record: r}
	}
	return response, err
}

// Capture only usage and routing metadata, not model text or auth headers.
type usageBody struct {
	io.ReadCloser
	pending []byte
	record  *record
}

func (b *usageBody) Read(out []byte) (int, error) {
	n, err := b.ReadCloser.Read(out)
	b.pending = append(b.pending, out[:n]...)
	for {
		end := bytes.IndexByte(b.pending, '\n')
		if end < 0 {
			break
		}
		line := b.pending[:end]
		b.pending = b.pending[end+1:]
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		var e struct {
			Type     string          `json:"type"`
			Model    string          `json:"model"`
			Usage    json.RawMessage `json:"usage"`
			Response struct {
				Model string          `json:"model"`
				Key   string          `json:"prompt_cache_key"`
				Usage json.RawMessage `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal(bytes.TrimSpace(line[5:]), &e) != nil {
			continue
		}
		if e.Type == "response.completed" {
			b.record.RawUsage = e.Response.Usage
			b.record.ResponseModel = e.Response.Model
			b.record.ReturnedKey = e.Response.Key
		} else if len(e.Usage) > 0 && string(e.Usage) != "null" {
			b.record.RawUsage = e.Usage
			b.record.ResponseModel = e.Model
		}
	}
	return n, err
}

func tokenCounts(raw json.RawMessage) (*int64, *int64) {
	var u struct {
		Input        *int64 `json:"input_tokens"`
		Prompt       *int64 `json:"prompt_tokens"`
		InputDetails struct {
			Cached *int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		PromptDetails struct {
			Cached *int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	_ = json.Unmarshal(raw, &u)
	if u.Input == nil {
		u.Input = u.Prompt
	}
	if u.InputDetails.Cached == nil {
		u.InputDetails.Cached = u.PromptDetails.Cached
	}
	return u.Input, u.InputDetails.Cached
}

// This case exercises SDK serialization directly, without NOE's body override.
// Responses supports a string input plus separate system instructions.
func nativeResponses(ctx context.Context, transport *recorder, credentialFile string, request provider.Request) (provider.Response, error) {
	secret, err := os.ReadFile(credentialFile)
	if err != nil {
		return provider.Response{}, err
	}
	key := strings.TrimSpace(string(secret))
	if key == "" {
		return provider.Response{}, fmt.Errorf("empty NOE credential file")
	}
	var system, user struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(request.Messages[0], &system); err != nil {
		return provider.Response{}, err
	}
	if err := json.Unmarshal(request.Messages[1], &user); err != nil {
		return provider.Response{}, err
	}
	input := components.CreateInputsUnionStr(user.Content)
	stream, maxTokens := true, int64(request.MaxTokens)
	effort, summary := components.ReasoningEffort(request.Effort), components.ReasoningSummaryVerbosityAuto
	reasoning := components.ReasoningConfig{Effort: optionalnullable.From(&effort), Summary: optionalnullable.From(&summary)}
	include := []components.ResponseIncludesEnum{components.ResponseIncludesEnumReasoningEncryptedContent}
	tier := components.ResponsesRequestServiceTierAuto
	body := components.ResponsesRequest{
		Model: &request.Model, Instructions: optionalnullable.From(&system.Content), Input: &input,
		Stream: &stream, MaxOutputTokens: optionalnullable.From(&maxTokens),
		Reasoning: optionalnullable.From(&reasoning), Include: optionalnullable.From(&include),
		ServiceTier: optionalnullable.From(&tier),
	}
	client := openrouter.New(openrouter.WithServerURL("https://openrouter.ai/api/v1"),
		openrouter.WithSecurity(key),
		openrouter.WithClient(&sessionClient{transport: transport, session: request.SessionID}),
		openrouter.WithRetryConfig(retry.Config{Strategy: "none"}))
	result, err := client.Responses.Send(ctx, body, nil,
		operations.WithRetries(retry.Config{Strategy: "none"}),
		operations.WithAcceptHeaderOverride(operations.AcceptHeaderEnumTextEventStream))
	if err != nil {
		return provider.Response{}, fmt.Errorf("SDK Responses: %s", strings.ReplaceAll(err.Error(), key, "[redacted]"))
	}
	if result == nil || result.EventStream == nil {
		return provider.Response{}, fmt.Errorf("missing SDK event stream")
	}
	defer result.EventStream.Close()
	for result.EventStream.Next() {
		event := result.EventStream.Value()
		if event == nil {
			continue
		}
		if completed := event.GetDataResponseCompleted(); completed != nil {
			data, err := json.Marshal(completed)
			if err != nil {
				return provider.Response{}, err
			}
			var parsed struct {
				Response struct {
					Usage  json.RawMessage `json:"usage"`
					Status string          `json:"status"`
				} `json:"response"`
			}
			if err := json.Unmarshal(data, &parsed); err != nil {
				return provider.Response{}, err
			}
			if parsed.Response.Status != "completed" {
				return provider.Response{}, fmt.Errorf("response status %q", parsed.Response.Status)
			}
			return provider.Response{Usage: parsed.Response.Usage, FinishReason: "stop"}, nil
		}
		if event.GetDataResponseFailed() != nil || event.GetDataResponseIncomplete() != nil {
			return provider.Response{}, fmt.Errorf("response failed or incomplete")
		}
	}
	if err := result.EventStream.Err(); err != nil {
		return provider.Response{}, fmt.Errorf("SDK stream: %s", strings.ReplaceAll(err.Error(), key, "[redacted]"))
	}
	return provider.Response{}, fmt.Errorf("stream ended without completion")
}

type sessionClient struct {
	transport *recorder
	session   string
}

func (c *sessionClient) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("x-session-id", c.session)
	req.Header.Set("User-Agent", "noe/sdk-cache-probe")
	return c.transport.Do(req)
}

func run() error {
	out := flag.String("out", "", "new artifact directory (required)")
	credentialDir := flag.String("credential-dir", "", "NOE credential directory (required)")
	selected := flag.String("cases", "chatgpt,opencode-chat,opencode-responses,openrouter", "comma-separated cases")
	flag.Parse()
	if !filepath.IsAbs(*out) || !filepath.IsAbs(*credentialDir) {
		return fmt.Errorf("out and credential-dir must be absolute paths")
	}
	if err := os.Mkdir(*out, 0700); err != nil {
		return err
	}
	var results []*trial
	versions := map[string]string{}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			if strings.Contains(d.Path, "openai-go") || strings.Contains(d.Path, "OpenRouterTeam") {
				versions[d.Path] = d.Version
			}
		}
	}
	save := func() error {
		data, err := json.MarshalIndent(map[string]any{"sdkVersions": versions, "trials": results}, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(*out, "results.json"), append(data, '\n'), 0600)
	}
	for _, name := range strings.Split(*selected, ",") {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		id := hex.EncodeToString(nonce[:])
		t := &trial{Name: name, SessionID: id, RequestEncoding: "noe-adapter"}
		profile := provider.Definition{Auth: "api-key", Headers: map[string]string{"User-Agent": "noe/sdk-cache-probe"}}
		effort := "high"
		switch name {
		case "chatgpt":
			t.Provider, t.Model, t.API, t.SDK = "openai-chatgpt", "gpt-6.1-sol", "responses", "openai"
			profile.Auth = "chatgpt-oauth"
			profile.OAuthFile = filepath.Join(*credentialDir, "openai-chatgpt.json")
			t.CredentialFile = profile.OAuthFile
			profile.BaseURL = "https://api.openai.com/v1"
			profile.SessionHeader = "session_id"
			profile.SessionField = "prompt_cache_key"
		case "opencode-chat", "opencode-responses":
			t.Provider, t.Model, t.API, t.SDK = "opencode-go", "deepseek-v4.1-flash", "chat-completions", "openrouter"
			if name == "opencode-responses" {
				t.Model, t.API = "gpt-6-luna", "responses"
			}
			profile.BaseURL = "https://opencode.ai/inference/go/openai/v1"
			profile.KeyFile = filepath.Join(*credentialDir, "opencode-go-api-key")
			profile.SessionHeader = "x-opencode-session"
		case "openrouter", "openrouter-responses":
			t.Provider, t.Model, t.API, t.SDK = "openrouter", "deepseek/deepseek-v4.1-flash", "chat-completions", "openrouter"
			profile.BaseURL = "https://openrouter.ai/api/v1"
			profile.KeyFile = filepath.Join(*credentialDir, "openrouter-api-key")
			profile.SessionField = "session_id"
			if strings.HasPrefix(name, "openrouter-responses") {
				t.API = "responses"
				if name == "openrouter-responses" {
					t.RequestEncoding = "sdk-native"
				}
				profile.SessionField = ""
				profile.SessionHeader = "x-session-id"
			}
		default:
			return fmt.Errorf("unknown case %q", name)
		}
		profile.SDK, profile.API, profile.Path = t.SDK, t.API, "/chat/completions"
		profile.EffortField = "reasoning.effort"
		profile.ReasoningField = "reasoning"
		if t.API == "responses" {
			profile.Path = "/responses"
			profile.Parameters = map[string]json.RawMessage{"reasoning": json.RawMessage(`{"summary":"auto"}`)}
		}
		if name == "opencode-chat" {
			profile.EffortField = "reasoning_effort"
			profile.ReasoningField = "reasoning_content"
		}
		if t.CredentialFile == "" {
			t.CredentialFile = profile.KeyFile
		}
		if info, err := os.Stat(t.CredentialFile); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("NOE credential file unavailable: %s", t.CredentialFile)
		}
		results = append(results, t)
		transport := &recorder{trial: t, header: profile.SessionHeader}
		opts := provider.Options{Profile: &profile, BaseURL: profile.BaseURL, KeyFile: profile.KeyFile, HTTPClient: transport}
		var sdk provider.Provider
		if t.SDK == "openai" {
			sdk = provider.NewOpenAI(opts)
		} else {
			sdk = provider.NewOpenRouter(opts)
		}
		var input strings.Builder
		input.WriteString("Review this JavaScript function. Its contract is to accept only finite numeric amounts and return a numeric total. Identify one concrete contract violation in one short sentence. Fixture rows below are reference data; do not sum them.\n\nexport function totalAmounts(rows) {\n  let total = 0;\n  for (const row of rows) {\n    if (!isFinite(row.amount)) throw new Error('invalid amount');\n    total += row.amount;\n  }\n  return total;\n}\n\n// Fixture rows:\n")
		for i := 0; i < 256; i++ {
			fmt.Fprintf(&input, "// row %03d: {id: 'record-%03d', category: 'category-%02d', amount: %d}\n", i, i, i%11, i%37)
		}
		system, _ := json.Marshal(map[string]string{"role": "system", "content": "Coding review probe " + id + ". Review the supplied source and follow the requested concise output format."})
		user, _ := json.Marshal(map[string]string{"role": "user", "content": input.String()})
		request := provider.Request{Provider: t.Provider, SessionID: id, CacheKey: id, Model: t.Model, Effort: effort, MaxTokens: 2048, Messages: []json.RawMessage{system, user}}
		count := 3
		for i := 0; i < count; i++ {
			request.RunID = fmt.Sprintf("%s-%d", id, i+1)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			var response provider.Response
			var err error
			if name == "openrouter-responses" {
				response, err = nativeResponses(ctx, transport, t.CredentialFile, request)
			} else {
				response, err = sdk.Generate(ctx, request)
			}
			cancel()
			if err != nil {
				t.Error = err.Error()
				_ = save()
				fmt.Printf("%s request=%d error=%s\n", name, i+1, t.Error)
				break
			}
			t.ParsedUsage = append(t.ParsedUsage, response.Usage)
			if response.FinishReason != "stop" {
				t.Error = "nonfinal response: " + response.FinishReason
			}
			current := t.Requests[len(t.Requests)-1]
			if current.BodyHash != t.Requests[0].BodyHash {
				t.Error = "request body changed"
			}
			if profile.SessionHeader != "" && current.SessionHeader != id {
				t.Error = "session header changed or missing"
			}
			if profile.SessionField != "" && current.BodySession != id {
				t.Error = "body session changed or missing"
			}
			inputTokens, cached := tokenCounts(response.Usage)
			counts := "not reported"
			if inputTokens != nil && cached != nil {
				counts = fmt.Sprintf("input=%d cached=%d rate=%.2f%%", *inputTokens, *cached, 100*float64(*cached)/float64(*inputTokens))
			}
			fmt.Printf("%s request=%d HTTP=%d %s\n", name, i+1, current.HTTPStatus, counts)
			if err := save(); err != nil {
				return err
			}
			if t.Error != "" {
				break
			}
			if i < 2 {
				time.Sleep(2 * time.Second)
			}
		}
	}
	if err := save(); err != nil {
		return err
	}
	for _, result := range results {
		if result.Error != "" {
			return fmt.Errorf("case %s failed; see results.json", result.Name)
		}
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
