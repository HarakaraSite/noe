package agentimages_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/models/operations"
	"github.com/OpenRouterTeam/go-sdk/retry"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type wire struct {
	URL              string   `json:"url"`
	Status           int      `json:"status"`
	ContentType      string   `json:"contentType,omitempty"`
	UserAgent        string   `json:"userAgent"`
	Session          string   `json:"session,omitempty"`
	Events           []string `json:"events,omitempty"`
	Terminal         string   `json:"terminal,omitempty"`
	IncompleteReason string   `json:"incompleteReason,omitempty"`
	OutputTypes      []string `json:"outputTypes,omitempty"`
	ErrorCode        string   `json:"errorCode,omitempty"`
	ErrorParam       string   `json:"errorParam,omitempty"`
	ErrorMessage     string   `json:"errorMessage,omitempty"`
}
type observedClient struct{ calls []*wire }

type observedBody struct {
	io.ReadCloser
	data bytes.Buffer
	wire *wire
}

func (b *observedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.data.Write(p[:n])
	return n, err
}
func (b *observedBody) Close() error {
	err := b.ReadCloser.Close()
	parse := func(data []byte) {
		var entry struct {
			Type  string `json:"type"`
			Error struct {
				Code    string `json:"code"`
				Param   string `json:"param"`
				Message string `json:"message"`
			} `json:"error"`
			Response struct {
				Status     string `json:"status"`
				Incomplete struct {
					Reason string `json:"reason"`
				} `json:"incomplete_details"`
				Output []struct {
					Type string `json:"type"`
				} `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal(data, &entry) != nil {
			return
		}
		if entry.Type != "" && (len(b.wire.Events) == 0 || b.wire.Events[len(b.wire.Events)-1] != entry.Type) {
			b.wire.Events = append(b.wire.Events, entry.Type)
		}
		if entry.Response.Status != "" {
			b.wire.Terminal = entry.Response.Status
		}
		if entry.Response.Incomplete.Reason != "" {
			b.wire.IncompleteReason = entry.Response.Incomplete.Reason
		}
		for _, item := range entry.Response.Output {
			b.wire.OutputTypes = append(b.wire.OutputTypes, item.Type)
		}
		if entry.Error.Code != "" {
			b.wire.ErrorCode = entry.Error.Code
		}
		if entry.Error.Param != "" {
			b.wire.ErrorParam = entry.Error.Param
		}
		if entry.Error.Message != "" {
			b.wire.ErrorMessage = entry.Error.Message
			if len(b.wire.ErrorMessage) > 400 {
				b.wire.ErrorMessage = b.wire.ErrorMessage[:400]
			}
		}
	}
	if bytes.HasPrefix(bytes.TrimSpace(b.data.Bytes()), []byte("{")) {
		parse(b.data.Bytes())
	} else {
		for _, line := range bytes.Split(b.data.Bytes(), []byte("\n")) {
			if bytes.HasPrefix(line, []byte("data: ")) {
				parse(line[len("data: "):])
			}
		}
	}
	b.data.Reset()
	return err
}

func (c *observedClient) Do(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	r.Header.Set("User-Agent", "noe/1.6.1")
	response, err := http.DefaultClient.Do(r)
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	record := &wire{URL: r.URL.Scheme + "://" + r.URL.Host + r.URL.Path, Status: status, UserAgent: r.Header.Get("User-Agent"), Session: r.Header.Get("x-opencode-session")}
	if response != nil {
		record.ContentType = response.Header.Get("Content-Type")
	}
	c.calls = append(c.calls, record)
	if response != nil && response.Body != nil {
		response.Body = &observedBody{ReadCloser: response.Body, wire: record}
	}
	return response, err
}

type outcome struct {
	Case       string          `json:"case"`
	Success    bool            `json:"success"`
	DurationMS int64           `json:"durationMs"`
	Error      string          `json:"error,omitempty"`
	Model      string          `json:"model,omitempty"`
	MaxTokens  int             `json:"maxTokens,omitempty"`
	Text       string          `json:"text,omitempty"`
	ImageMatch *bool           `json:"imageMatch,omitempty"`
	Usage      json.RawMessage `json:"usage,omitempty"`
	Path       string          `json:"path,omitempty"`
	Bytes      int             `json:"bytes,omitempty"`
	Width      int             `json:"width,omitempty"`
	Height     int             `json:"height,omitempty"`
	Format     string          `json:"format,omitempty"`
	Events     []string        `json:"events,omitempty"`
	Wire       []*wire         `json:"wire"`
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func saveImage(dir, name, encoded string, out *outcome) error {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode image header: %w", err)
	}
	path := filepath.Join(dir, name+"."+format)
	if err = os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	out.Path, out.Bytes, out.Width, out.Height, out.Format = path, len(data), config.Width, config.Height, format
	return nil
}
func keyPath(providerID string) string {
	home, _ := os.UserHomeDir()
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	return filepath.Join(config, "noe", "credentials", providerID)
}

func explicitProviderOptions(t *testing.T, client provider.HTTPClient, userAgent string) provider.Options {
	t.Helper()
	env := map[string]string{}
	for _, key := range []string{
		"NOE_CONFIG_FILE", "XDG_CONFIG_HOME", "HOME",
		"NOE_OPENROUTER_API_KEY_FILE", "NOE_OPENCODE_GO_API_KEY_FILE",
		"NOE_OPENAI_API_KEY_FILE", "NOE_OPENAI_CHATGPT_CREDENTIALS_FILE",
	} {
		if value, ok := os.LookupEnv(key); ok {
			env[key] = value
		}
	}
	baseDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	configHome := env["XDG_CONFIG_HOME"]
	if configHome == "" {
		home := env["HOME"]
		if home == "" {
			home, err = os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
		}
		configHome = filepath.Join(home, ".config")
	}
	configDir := filepath.Join(configHome, "noe")
	configFile := env["NOE_CONFIG_FILE"]
	if configFile == "" {
		candidate := filepath.Join(configDir, "config.json")
		if _, statErr := os.Stat(candidate); statErr == nil {
			configFile = candidate
		} else if !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("inspect default provider configuration: %v", statErr)
		}
	}
	return provider.Options{
		BaseDir: baseDir, ConfigFile: configFile, ConfigDir: configDir,
		Env: env, HTTPClient: client, UserAgent: userAgent,
	}
}

func openAIClient(token string, client *observedClient) openai.Client {
	return openai.NewClient(option.WithBaseURL("https://api.openai.com/v1"), option.WithAPIKey(token), option.WithAdminAPIKey(""), option.WithOrganization(""), option.WithProject(""), option.WithHTTPClient(client), option.WithMaxRetries(0))
}

// Explicit opt-in only: this test makes paid/subscription provider requests.
// Results retain metadata and image files, never tokens or base64 request bodies.
func TestImageProviderSpike(t *testing.T) {
	if os.Getenv("NOE_IMAGE_SPIKE_LIVE") != "1" {
		t.Skip("set NOE_IMAGE_SPIKE_LIVE=1 for real provider requests")
	}
	dir := os.Getenv("NOE_IMAGE_SPIKE_DIR")
	if dir == "" {
		t.Fatal("NOE_IMAGE_SPIKE_DIR must identify an artifact directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Log("artifacts:", dir)
	fixture := image.NewRGBA(image.Rect(0, 0, 128, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 128; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if x >= 64 {
				c = color.RGBA{0, 0, 255, 255}
			}
			fixture.SetRGBA(x, y, c)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, fixture); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "input.png"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	cases := []string{"vision-openrouter", "vision-opencode-go", "vision-openai-api", "vision-chatgpt", "image-openrouter", "image-openai-api", "image-chatgpt-direct", "image-chatgpt-responses"}
	if selected := os.Getenv("NOE_IMAGE_SPIKE_CASES"); selected != "" {
		cases = strings.Split(selected, ",")
	}
	var results []outcome
	original, readErr := credentials.Read(keyPath("openai-chatgpt.json"))
	// If this approved test refreshes a shared token, synchronize the matching hjh
	// record as previously authorized. Never replace another account/token chain.
	defer func() {
		updated, err := credentials.Read(keyPath("openai-chatgpt.json"))
		if readErr != nil || err != nil || updated.Refresh == original.Refresh {
			return
		}
		home, _ := os.UserHomeDir()
		paths, _ := filepath.Glob(filepath.Join(home, ".config", "hjh", "credentials", "chatgpt", "*.json"))
		for _, path := range paths {
			err := credentials.WithFileLock(context.Background(), path, func() error {
				old, err := credentials.Read(path)
				if err != nil || old.Refresh != original.Refresh || old.Subject != original.Subject || old.ClientID != original.ClientID {
					return nil
				}
				return credentials.Write(path, updated)
			})
			if err != nil {
				t.Errorf("synchronize refreshed matching hjh record: %v", err)
			}
		}
	}()
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			defer cancel()
			start := time.Now()
			out := outcome{Case: name}
			client := &observedClient{}
			var err error
			if strings.HasPrefix(name, "vision-") {
				id, model := "openrouter", "openai/gpt-6-luna"
				if name == "vision-opencode-go" {
					id, model = "opencode-go", "gpt-6-luna"
				}
				if name == "vision-openai-api" {
					id, model = "openai-api", "gpt-6-luna"
				}
				if name == "vision-chatgpt" {
					id, model = "openai-chatgpt", "gpt-6-luna"
				}
				out.Model = model
				textType, imageType := "text", "image_url"
				var imagePart any = map[string]any{"type": imageType, "image_url": map[string]any{"url": dataURL}}
				if id != "openrouter" {
					textType = "input_text"
					imagePart = map[string]any{"type": "input_image", "image_url": dataURL, "detail": "low"}
				}
				content := []any{map[string]any{"type": textType, "text": "Name the two large colors in this image from left to right. Answer only the two color names."}, imagePart}
				registry := provider.NewExplicitRegistry(explicitProviderOptions(t, client, "noe/1.6.1"))
				var answer provider.Response
				maxTokens := 4096
				if value := os.Getenv("NOE_IMAGE_SPIKE_MAX_TOKENS"); value != "" {
					parsed, parseErr := strconv.Atoi(value)
					if parseErr != nil || parsed <= 0 {
						t.Fatal("NOE_IMAGE_SPIKE_MAX_TOKENS must be a positive integer")
					}
					maxTokens = parsed
				}
				out.MaxTokens = maxTokens
				answer, err = registry.Generate(ctx, provider.Request{Provider: id, Model: model, RunID: "image-spike-" + name, CacheKey: "image-spike-" + name, Effort: "auto", MaxTokens: maxTokens, Messages: []json.RawMessage{raw(map[string]any{"role": "user", "content": content})}})
				if err == nil {
					var message struct {
						Content string `json:"content"`
					}
					err = json.Unmarshal(answer.Message, &message)
					out.Text = message.Content
					out.Usage = answer.Usage
					matches := strings.Index(strings.ToLower(out.Text), "red") >= 0 && strings.Index(strings.ToLower(out.Text), "red") < strings.Index(strings.ToLower(out.Text), "blue")
					out.ImageMatch = &matches
					if !strings.Contains(strings.ToLower(out.Text), "red") || !strings.Contains(strings.ToLower(out.Text), "blue") {
						err = errors.New("image colors were not identified")
					}
				}
			} else if name == "image-openrouter" {
				out.Model = "openai/gpt-image-2.5-flare"
				var key []byte
				key, err = os.ReadFile(keyPath("openrouter-api-key"))
				if err == nil {
					sdk := openrouter.New(openrouter.WithSecurity(strings.TrimSpace(string(key))), openrouter.WithClient(client))
					n := int64(1)
					quality := components.ImageGenerationRequestQualityLow
					size := "1024x1024"
					session := "image-spike-openrouter"
					var result *operations.CreateImagesResponse
					result, err = sdk.Images.Generate(ctx, components.ImageGenerationRequest{Model: out.Model, Prompt: "A simple flat icon: one red circle on a plain white background. No text.", N: &n, Quality: &quality, Size: &size, SessionID: &session}, operations.WithRetries(retry.Config{Strategy: "none"}))
					if err == nil {
						if result == nil || result.ImageGenerationResponse == nil || len(result.ImageGenerationResponse.Data) != 1 {
							err = errors.New("expected one buffered image")
						} else {
							response := result.ImageGenerationResponse
							out.Usage = raw(response.Usage)
							err = saveImage(dir, name, response.Data[0].B64JSON, &out)
						}
					}
				}
			} else {
				var token string
				if name == "image-openai-api" {
					var key []byte
					key, err = os.ReadFile(keyPath("openai-api-key"))
					token = strings.TrimSpace(string(key))
				} else {
					token, err = credentials.NewChatGPT(keyPath("openai-chatgpt.json"), credentials.Options{HTTPClient: client}).Token(ctx)
				}
				if err == nil {
					sdk := openAIClient(token, client)
					if name == "image-chatgpt-responses" {
						out.Model = "gpt-6.1-sol"
						body := raw(map[string]any{"model": out.Model, "input": []any{map[string]any{"role": "user", "content": []any{map[string]string{"type": "input_text", "text": "Generate one image: a simple flat icon of one red circle on a plain white background. No text."}}}}, "stream": true, "store": false, "reasoning": map[string]string{"effort": "low"}, "tools": []any{map[string]any{"type": "image_generation", "model": "gpt-image-2.5-flare", "quality": "low", "size": "1024x1024"}}, "tool_choice": map[string]string{"type": "image_generation"}})
						stream := sdk.Responses.NewStreaming(ctx, responses.ResponseNewParams{}, option.WithRequestBody("application/json", []byte(body)))
						defer stream.Close()
						completed := false
						for stream.Next() {
							e := stream.Current()
							out.Events = append(out.Events, e.Type)
							if e.Type == "response.output_item.done" {
								var event struct {
									Item struct {
										Type   string `json:"type"`
										Result string `json:"result"`
									} `json:"item"`
								}
								if json.Unmarshal([]byte(e.RawJSON()), &event) == nil && event.Item.Type == "image_generation_call" && event.Item.Result != "" {
									err = saveImage(dir, name, event.Item.Result, &out)
									if err != nil {
										break
									}
								}
							}
							if e.Type == "response.failed" || e.Type == "response.incomplete" {
								var event struct {
									Response struct {
										Error struct {
											Code    string `json:"code"`
											Message string `json:"message"`
										} `json:"error"`
									} `json:"response"`
								}
								json.Unmarshal([]byte(e.RawJSON()), &event)
								err = fmt.Errorf("%s: %s %s", e.Type, event.Response.Error.Code, event.Response.Error.Message)
								break
							}
							if e.Type == "response.completed" {
								completed = true
								var event struct {
									Response struct {
										Usage json.RawMessage `json:"usage"`
									} `json:"response"`
								}
								json.Unmarshal([]byte(e.RawJSON()), &event)
								out.Usage = event.Response.Usage
								break
							}
						}
						if err == nil {
							err = stream.Err()
						}
						stream.Close()
						if err == nil && (!completed || out.Path == "") {
							err = errors.New("no completed image generation result")
						}
					} else {
						out.Model = "gpt-image-2.5-flare"
						var response *openai.ImagesResponse
						response, err = sdk.Images.Generate(ctx, openai.ImageGenerateParams{Model: out.Model, Prompt: "A simple flat icon: one red circle on a plain white background. No text.", N: openai.Int(1), Quality: "low", Size: "1024x1024", OutputFormat: "png"})
						if err == nil {
							if len(response.Data) != 1 {
								err = errors.New("expected one generated image")
							} else {
								out.Usage = raw(response.Usage)
								err = saveImage(dir, name, response.Data[0].B64JSON, &out)
							}
						}
					}
					if err != nil {
						message := err.Error()
						var apiError *openai.Error
						if errors.As(err, &apiError) {
							message = fmt.Sprintf("HTTP %d: code=%s param=%s message=%s", apiError.StatusCode, apiError.Code, apiError.Param, apiError.Message)
							if apiError.Response != nil && apiError.Response.Body != nil {
								body, _ := io.ReadAll(apiError.Response.Body)
								apiError.Response.Body.Close()
								if len(body) > 0 {
									message += " body=" + string(body)
								}
							}
						}
						out.Error = strings.ReplaceAll(message, token, "[redacted]")
					}
				}
			}
			out.DurationMS = time.Since(start).Milliseconds()
			out.Success = err == nil
			out.Wire = client.calls
			if err != nil && out.Error == "" {
				out.Error = err.Error()
			}
			out.Error = strings.ReplaceAll(out.Error, dataURL, "[image]")
			if len(out.Error) > 1800 {
				out.Error = out.Error[:1800]
			}
			results = append(results, out)
			report, _ := json.MarshalIndent(results, "", "  ")
			if err := os.WriteFile(filepath.Join(dir, "report.json"), report, 0600); err != nil {
				t.Fatal(err)
			}
			summary, _ := json.Marshal(out)
			t.Log(string(summary))
		})
	}
}
