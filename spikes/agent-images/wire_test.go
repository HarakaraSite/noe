package agentimages_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/runtime"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type imageWireCheck struct {
	Model       string   `json:"model"`
	Path        string   `json:"path"`
	ImageSHA256 string   `json:"imageSHA256,omitempty"`
	Width       int      `json:"width,omitempty"`
	Height      int      `json:"height,omitempty"`
	PartType    string   `json:"partType,omitempty"`
	Keys        []string `json:"keys,omitempty"`
}
type imageWireClient struct {
	mu       sync.Mutex
	requests []imageWireCheck
}

func (c *imageWireClient) Do(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	var root map[string]any
	_ = json.Unmarshal(body, &root)
	record := imageWireCheck{Path: req.URL.Host + req.URL.Path}
	record.Model, _ = root["model"].(string)
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case []any:
			for _, child := range v {
				walk(child)
			}
		case map[string]any:
			kind, _ := v["type"].(string)
			if kind == "input_image" || kind == "image_url" {
				url, _ := v["image_url"].(string)
				if image, ok := v["image_url"].(map[string]any); ok {
					url, _ = image["url"].(string)
				}
				parts := strings.SplitN(url, ",", 2)
				if len(parts) == 2 {
					data, e := base64.StdEncoding.DecodeString(parts[1])
					if e == nil {
						digest := sha256.Sum256(data)
						record.ImageSHA256 = hex.EncodeToString(digest[:])
						config, _, _ := image.DecodeConfig(bytes.NewReader(data))
						record.Width, record.Height = config.Width, config.Height
						record.PartType = kind
						for key := range v {
							record.Keys = append(record.Keys, key)
						}
						sort.Strings(record.Keys)
					}
				}
			}
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(root)
	c.mu.Lock()
	c.requests = append(c.requests, record)
	c.mu.Unlock()
	return http.DefaultClient.Do(req)
}
func TestImageWireLiveDiagnostic(t *testing.T) {
	if os.Getenv("NOE_IMAGE_WIRE_LIVE") != "1" {
		t.Skip("explicit live authorization and NOE_IMAGE_WIRE_LIVE=1 required")
	}
	directory := os.Getenv("NOE_IMAGE_WIRE_DIR")
	executable := os.Getenv("NOE_IMAGE_WIRE_EXECUTABLE")
	if directory == "" || executable == "" {
		t.Fatal("wire diagnostic directory and executable required")
	}
	path := filepath.Join(directory, "input-followup.png")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(data)
	client := &imageWireClient{}
	registry := provider.NewRegistry(provider.Options{HTTPClient: client})
	var results []map[string]any
	directModels := []string{"gpt-6-luna", "gpt-6.1-sol"}
	if os.Getenv("NOE_IMAGE_WIRE_AGENT_ONLY") == "1" {
		directModels = nil
	}
	for _, model := range directModels {
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		message, _ := json.Marshal(map[string]any{"role": "user", "content": []any{map[string]string{"type": "text", "text": "Identify the large solid color in each quadrant of the attached image. Return JSON with topLeft, topRight, bottomLeft, bottomRight color names only."}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), "detail": "high"}}}})
		reply, callErr := registry.Generate(ctx, provider.Request{Provider: "openai-api", Model: model, Effort: "low", MaxTokens: 4096, Messages: []json.RawMessage{message}})
		cancel()
		var answer struct {
			Content json.RawMessage `json:"content"`
			Refusal string          `json:"refusal"`
		}
		_ = json.Unmarshal(reply.Message, &answer)
		result := map[string]any{"mode": "direct", "model": model, "content": answer.Content, "refusal": answer.Refusal, "usage": reply.Usage}
		if callErr != nil {
			result["error"] = callErr.Error()
		}
		results = append(results, result)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	encodedPath, _ := json.Marshal(path)
	source := `import {run_agent} from "noe:agent";
const r=await run_agent({provider:"openai-api",model:"gpt-6-luna",effort:"low",maxSteps:2,maxTokens:4096},'Read the image at '+` + string(encodedPath) + `+' using read_image from noe:image in run_js. The image is attached to your next model request. Do not decode its file yourself. Identify the large solid color in each quadrant. Return JSON with topLeft,topRight,bottomLeft,bottomRight color names only.');console.log(JSON.stringify(r.lastResult));`
	agentModel := os.Getenv("NOE_IMAGE_WIRE_AGENT_MODEL")
	if agentModel == "" {
		agentModel = "gpt-6-luna"
	}
	encodedModel, _ := json.Marshal(agentModel)
	source = strings.Replace(source, `model:"gpt-6-luna"`, "model:"+string(encodedModel), 1)
	var stdout bytes.Buffer
	code, runErr := runtime.Execute(ctx, runtime.Source{Name: filepath.Join(directory, "wire-agent.js"), Code: source}, runtime.Options{Stdout: &stdout, Agent: &agent.Options{Provider: registry, Executable: executable}})
	result := map[string]any{"mode": "agent", "exitCode": code, "stdout": stdout.String()}
	if runErr != nil {
		result["error"] = runErr.Error()
	}
	results = append(results, result)
	client.mu.Lock()
	requests := append([]imageWireCheck(nil), client.requests...)
	client.mu.Unlock()
	imageRequests := 0
	for _, request := range requests {
		if request.ImageSHA256 != "" {
			imageRequests++
		}
		if request.ImageSHA256 != "" && request.ImageSHA256 != hex.EncodeToString(expected[:]) {
			t.Errorf("wire image differs from source: %+v", request)
		}
	}
	if imageRequests != len(directModels)+1 {
		t.Errorf("image wire count=%d want=%d", imageRequests, len(directModels)+1)
	}
	reportFile := "wire-diagnostic.json"
	if len(directModels) == 0 {
		reportFile = "wire-agent-" + strings.ReplaceAll(agentModel, "/", "-") + ".json"
	}
	report := map[string]any{"sourceSHA256": hex.EncodeToString(expected[:]), "requests": requests, "results": results}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(filepath.Join(directory, reportFile), append(encoded, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		fmt.Fprintln(t.Output(), result["mode"], result["model"], result["error"])
	}
}
