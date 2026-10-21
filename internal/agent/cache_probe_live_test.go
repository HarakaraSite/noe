//go:build agent_efficiency_live

package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

// A two-request control, independent of agent delegation and history growth.
func TestAgentCacheProbeLive(t *testing.T) {
	if os.Getenv("NOE_AGENT_EFFICIENCY_LIVE") != "1" {
		t.Skip("set NOE_AGENT_EFFICIENCY_LIVE=1 to use the ChatGPT plan")
	}
	root := os.Getenv("NOE_AGENT_EFFICIENCY_LIVE_DIR")
	if !filepath.IsAbs(root) {
		t.Fatal("NOE_AGENT_EFFICIENCY_LIVE_DIR must be absolute")
	}
	dir := filepath.Join(root, "cache_probe")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	client := &cacheProbeClient{}
	defer func() {
		data, _ := json.MarshalIndent(client.records, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "responses.json"), append(data, '\n'), 0600); err != nil {
			t.Error(err)
		}
	}()
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	p := provider.NewExplicitRegistry(provider.Options{
		BaseDir: root, ConfigDir: filepath.Join(configDir, "noe"), HTTPClient: client,
		Env: map[string]string{
			"NOE_OPENAI_CHATGPT_CREDENTIALS_FILE": os.Getenv("NOE_OPENAI_CHATGPT_CREDENTIALS_FILE"),
		},
	})
	system, _ := json.Marshal(map[string]string{"role": "system", "content": Facts()})
	user := json.RawMessage(`{"role":"user","content":"Reply with exactly OK. No tools or other work are needed."}`)
	key := fmt.Sprintf("noe-efficiency-probe-%d", time.Now().UnixNano())
	request := provider.Request{Provider: "openai-chatgpt", RunID: key, SessionID: key, CacheKey: key, Model: "gpt-6.1-sol", Effort: "high", MaxTokens: 1024, Messages: []json.RawMessage{system, user}}
	for index := 0; index < 2; index++ {
		request.RunID = fmt.Sprintf("%s-task-%d", key, index+1)
		previousRecords := len(client.records)
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		response, err := p.Generate(ctx, request)
		cancel()
		if len(client.records) > previousRecords {
			client.records[len(client.records)-1].RunID = request.RunID
		}
		if err != nil {
			t.Fatalf("probe %d: %v", index+1, err)
		}
		if len(client.records) == previousRecords {
			t.Fatalf("probe %d completed without an HTTP request record", index+1)
		}
		record := client.records[len(client.records)-1]
		if record.SessionHeader != key {
			t.Fatalf("probe %d session_id header = %q, want %q", index+1, record.SessionHeader, key)
		}
		record.ParsedUsage = response.Usage
		if cacheProbeHash(record.RawUsage) != cacheProbeHash(response.Usage) {
			t.Fatal("adapter usage differs from the raw response usage")
		}
		var usage struct {
			Input   int64 `json:"input_tokens"`
			Details struct {
				Cached  int64 `json:"cached_tokens"`
				Written int64 `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
		}
		_ = json.Unmarshal(response.Usage, &usage)
		t.Logf("probe %d input=%d cached=%d cacheWrite=%d", index+1, usage.Input, usage.Details.Cached, usage.Details.Written)
	}
	if len(client.records) != 2 || client.records[0].InputHash != client.records[1].InputHash {
		t.Fatal("identical-input control was not preserved")
	}
	if client.records[0].RunID == client.records[1].RunID || client.records[0].SessionHeader != client.records[1].SessionHeader {
		t.Fatal("run ID should change while the session ID stays fixed")
	}
}

type cacheProbeRecord struct {
	RunID         string          `json:"runId"`
	InputHash     string          `json:"inputHash"`
	SessionHeader string          `json:"sessionHeader"`
	Model         string          `json:"model"`
	CacheKey      string          `json:"cacheKey"`
	HTTPStatus    int             `json:"httpStatus"`
	RawUsage      json.RawMessage `json:"rawUsage,omitempty"`
	ParsedUsage   json.RawMessage `json:"parsedUsage,omitempty"`
}

type cacheProbeClient struct {
	records []*cacheProbeRecord
}

func (c *cacheProbeClient) Do(req *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(req.URL.Path, "/responses") {
		return http.DefaultClient.Do(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	var request struct {
		Input          json.RawMessage `json:"input"`
		Model          string          `json:"model"`
		PromptCacheKey string          `json:"prompt_cache_key"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	record := &cacheProbeRecord{
		InputHash: cacheProbeHash(request.Input), SessionHeader: req.Header.Get("session_id"),
		Model: request.Model, CacheKey: request.PromptCacheKey,
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	c.records = append(c.records, record)
	response, err := http.DefaultClient.Do(req)
	if response != nil {
		record.HTTPStatus = response.StatusCode
		response.Body = &cacheProbeBody{ReadCloser: response.Body, record: record}
	}
	return response, err
}

type cacheProbeBody struct {
	io.ReadCloser
	pending []byte
	record  *cacheProbeRecord
}

func (b *cacheProbeBody) Read(out []byte) (int, error) {
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
		var event struct {
			Type     string `json:"type"`
			Response struct {
				Usage json.RawMessage `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal(bytes.TrimSpace(line[5:]), &event) == nil && event.Type == "response.completed" {
			b.record.RawUsage = event.Response.Usage
		}
	}
	return n, err
}

// Compare canonical JSON without retaining or logging prompt or credential data.
func cacheProbeHash(value json.RawMessage) string {
	var decoded any
	_ = json.Unmarshal(value, &decoded)
	canonical, _ := json.Marshal(decoded)
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:])
}
