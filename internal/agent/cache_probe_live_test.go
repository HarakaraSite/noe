//go:build agent_efficiency_live

package agent

import (
	"bytes"
	"context"
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
// Diagnostic options affect only this explicitly selected live test.
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
	client := &cacheProbeClient{diagnostics: os.Getenv("NOE_AGENT_CACHE_PROBE_DIAGNOSTICS") == "1"}
	defer func() {
		data, _ := json.MarshalIndent(client.records, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "responses.json"), append(data, '\n'), 0600); err != nil {
			t.Error(err)
		}
	}()
	p := provider.NewRegistry(provider.Options{HTTPClient: client})
	system, _ := json.Marshal(map[string]string{"role": "system", "content": Instructions()})
	user := json.RawMessage(`{"role":"user","content":"Reply with exactly OK. No tools or other work are needed."}`)
	key := fmt.Sprintf("noe-efficiency-probe-%d", time.Now().UnixNano())
	request := provider.Request{Provider: "openai-chatgpt", RunID: key, SessionID: key, CacheKey: key, Model: "gpt-6.1-sol", Effort: "high", MaxTokens: 1024, Messages: []json.RawMessage{system, user}}
	for index := 0; index < 2; index++ {
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		response, err := p.Generate(ctx, request)
		cancel()
		if err != nil {
			if client.diagnostics && len(client.records) > 0 {
				last := client.records[len(client.records)-1]
				var failure struct {
					Param string `json:"param"`
				}
				_ = json.Unmarshal(last.APIError, &failure)
				if last.HTTPStatus == 400 && failure.Param == "prompt_cache_options" {
					t.Skip("provider rejected prompt_cache_options; raw API error recorded")
				}
			}
			t.Fatalf("probe %d: %v", index+1, err)
		}
		record := client.records[len(client.records)-1]
		record.ParsedUsage = response.Usage
		if efficiencyHash(record.RawUsage) != efficiencyHash(response.Usage) {
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
}

type cacheProbeRecord struct {
	InputHash         string          `json:"inputHash"`
	Model             string          `json:"model"`
	CacheKey          string          `json:"cacheKey"`
	DiagnosticOptions bool            `json:"diagnosticOptions"`
	ComparisonID      string          `json:"comparisonResponseId,omitempty"`
	ResponseID        string          `json:"responseId,omitempty"`
	HTTPStatus        int             `json:"httpStatus"`
	RawUsage          json.RawMessage `json:"rawUsage,omitempty"`
	ParsedUsage       json.RawMessage `json:"parsedUsage,omitempty"`
	Diagnostics       json.RawMessage `json:"diagnostics,omitempty"`
	APIError          json.RawMessage `json:"apiError,omitempty"`
}

type cacheProbeClient struct {
	records     []*cacheProbeRecord
	diagnostics bool
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
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	record := &cacheProbeRecord{InputHash: efficiencyHash(request["input"]), DiagnosticOptions: c.diagnostics}
	_ = json.Unmarshal(request["model"], &record.Model)
	_ = json.Unmarshal(request["prompt_cache_key"], &record.CacheKey)
	options := map[string]string{"mode": "implicit"}
	if len(c.records) > 0 {
		record.ComparisonID = c.records[len(c.records)-1].ResponseID
		if record.ComparisonID != "" {
			options["comparison_response_id"] = record.ComparisonID
		}
	}
	if c.diagnostics {
		request["prompt_cache_options"], _ = json.Marshal(options)
	}
	body, _ = json.Marshal(request)
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	c.records = append(c.records, record)
	response, err := http.DefaultClient.Do(req)
	if response != nil {
		record.HTTPStatus = response.StatusCode
		if response.StatusCode >= 400 {
			data, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			var failure struct {
				Error json.RawMessage `json:"error"`
			}
			_ = json.Unmarshal(data, &failure)
			record.APIError = failure.Error
			response.Body = io.NopCloser(bytes.NewReader(data))
		}
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
				ID          string          `json:"id"`
				Usage       json.RawMessage `json:"usage"`
				Diagnostics json.RawMessage `json:"prompt_cache_diagnostics"`
			} `json:"response"`
		}
		if json.Unmarshal(bytes.TrimSpace(line[5:]), &event) == nil && event.Type == "response.completed" {
			b.record.ResponseID = event.Response.ID
			b.record.RawUsage = event.Response.Usage
			b.record.Diagnostics = event.Response.Diagnostics
		}
	}
	return n, err
}
