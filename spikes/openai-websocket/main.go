package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
	openai "github.com/openai/openai-go/v3"
)

const endpoint = "https://api.openai.com/v1"

type authentication struct {
	base    http.RoundTripper
	token   func(context.Context) (string, error)
	mu      sync.Mutex
	secrets []string
}

func (a *authentication) RoundTrip(request *http.Request) (*http.Response, error) {
	token, err := a.token(request.Context())
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.secrets = append(a.secrets, token)
	a.mu.Unlock()
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+token)
	return a.base.RoundTrip(request)
}

func (a *authentication) redact(text string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, token := range a.secrets {
		if token != "" {
			text = strings.ReplaceAll(text, token, "[redacted]")
		}
	}
	return text
}

func nonce() string {
	var data [12]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data[:])
}

func liveAuthentication(client *http.Client, mode string) (*authentication, error) {
	environment, filename := "NOE_OPENAI_CHATGPT_CREDENTIALS_FILE", "openai-chatgpt.json"
	switch mode {
	case "chatgpt":
	case "api-key":
		environment, filename = "NOE_OPENAI_API_KEY_FILE", "openai-api-key"
	default:
		return nil, errors.New("auth must be chatgpt or api-key")
	}
	path := os.Getenv(environment)
	if path == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(base, "noe", "credentials", filename)
	}
	if mode == "api-key" {
		return &authentication{base: client.Transport, token: func(ctx context.Context) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			key := strings.TrimSpace(string(data))
			if key == "" {
				return "", errors.New("selected OpenAI API key is empty")
			}
			return key, nil
		}}, nil
	}
	auth := credentials.NewChatGPT(path, credentials.Options{HTTPClient: client})
	return &authentication{base: client.Transport, token: auth.Token}, nil
}

type report struct {
	Date           string           `json:"checkedAt"`
	Scenario       string           `json:"scenario"`
	Authentication string           `json:"authentication"`
	Provider       string           `json:"provider"`
	Endpoint       string           `json:"endpoint"`
	Model          string           `json:"model"`
	Effort         string           `json:"effort"`
	RequestedTier  string           `json:"requestedServiceTier"`
	ToolKind       string           `json:"toolKind"`
	ConnectMS      float64          `json:"connectMs"`
	Turns          []turn           `json:"turns"`
	Trials         []trial          `json:"trials,omitempty"`
	Lifecycle      []lifecycleCheck `json:"lifecycle,omitempty"`
	Passed         bool             `json:"passed"`
	Error          string           `json:"error,omitempty"`
	APIKeyFallback bool             `json:"apiKeyFallback"`
}

func smoke(ctx context.Context, client openai.Client, model, effort, tier, kind string, result *report) error {
	connection, connectMS, err := openSocket(ctx, client)
	result.ConnectMS = connectMS
	if err != nil {
		return err
	}
	defer connection.Close()
	key := "noe-ws-spike-" + nonce()
	input := []item{inputMessage("user", "Call the supplied tool once, then reply with its output verbatim. Do not add any other text.")}
	first, err := websocketTurn(ctx, connection, requestBody(model, effort, tier, key, kind, input, forcedTool(kind)))
	result.Turns = append(result.Turns, first)
	if err != nil {
		return err
	}
	value := "probe-" + nonce()
	output, err := toolOutput(first, kind, value)
	if err != nil {
		return err
	}
	request := requestBody(model, effort, tier, key, kind, []item{output}, "none")
	request["previous_response_id"] = first.ResponseID
	second, err := websocketTurn(ctx, connection, request)
	result.Turns = append(result.Turns, second)
	if err != nil {
		return err
	}
	return verifiedAnswer(second, value)
}

func main() {
	model := flag.String("model", "gpt-6.1-sol", "Model used for both transports")
	effort := flag.String("effort", "low", "Reasoning effort used for both transports")
	tier := flag.String("tier", "priority", "Requested service tier used for both transports")
	authMode := flag.String("auth", "chatgpt", "Authentication: chatgpt (OAuth) or api-key (OpenAI API billing); never fall back")
	kind := flag.String("tool", "", "Tool format: custom or function; defaults to the selected provider's format")
	scenario := flag.String("scenario", "smoke", "Experiment: smoke, compare, or lifecycle")
	pairs := flag.Int("pairs", 3, "Balanced SSE/WebSocket trial pairs")
	rounds := flag.Int("rounds", 3, "Synthetic tool outputs per comparison conversation")
	contextBytes := flag.Int("context-bytes", 16384, "Synthetic background context bytes per comparison")
	timeout := flag.Duration("timeout", 6*time.Minute, "Total experiment deadline")
	out := flag.String("out", "", "Write the sanitized JSON report to this file")
	flag.Parse()
	if *authMode != "chatgpt" && *authMode != "api-key" {
		fmt.Fprintln(os.Stderr, "auth must be chatgpt or api-key")
		os.Exit(2)
	}
	if *kind == "" {
		if *authMode == "api-key" {
			*kind = "function"
		} else {
			*kind = "custom"
		}
	}
	if *pairs <= 0 || *rounds <= 0 || *contextBytes < 0 || *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "invalid experiment size or timeout")
		os.Exit(2)
	}
	if *kind != "custom" && *kind != "function" {
		fmt.Fprintln(os.Stderr, "tool must be custom or function")
		os.Exit(2)
	}
	provider, authLabel := "openai-chatgpt", "ChatGPT OAuth plan sharing"
	if *authMode == "api-key" {
		provider, authLabel = "openai-api", "explicit OpenAI API key"
	}
	result := report{Date: time.Now().UTC().Format(time.RFC3339), Scenario: *scenario, Provider: provider, Authentication: authLabel, Endpoint: endpoint + "/responses", Model: *model, Effort: *effort, RequestedTier: *tier, ToolKind: *kind}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	base := http.DefaultTransport.(*http.Transport).Clone()
	defer base.CloseIdleConnections()
	plain := &http.Client{Transport: base, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	auth, err := liveAuthentication(plain, *authMode)
	if err == nil {
		session := ""
		if *authMode == "chatgpt" {
			session = "noe-ws-spike-" + nonce()
		}
		client := newClient(endpoint, session, &http.Client{Transport: auth, CheckRedirect: plain.CheckRedirect})
		switch *scenario {
		case "smoke":
			err = smoke(ctx, client, *model, *effort, *tier, *kind, &result)
		case "compare":
			err = compare(ctx, client, *model, *effort, *tier, *kind, *pairs, *rounds, *contextBytes, &result)
		case "lifecycle":
			err = lifecycle(ctx, client, *model, *effort, *tier, *kind, &result)
		default:
			err = errors.New("unknown scenario")
		}
	}
	if err != nil {
		var api *openai.Error
		if errors.As(err, &api) {
			result.Error = fmt.Sprintf("HTTP %d %s: %s", api.StatusCode, api.Code, api.Message)
		} else {
			result.Error = err.Error()
		}
		if auth != nil {
			result.Error = auth.redact(result.Error)
		}
	} else {
		result.Passed = true
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	if auth != nil {
		data = []byte(auth.redact(string(data)))
	}
	data = append(data, '\n')
	if *out != "" {
		if err := os.WriteFile(*out, data, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	os.Stdout.Write(data)
	if !result.Passed {
		os.Exit(1)
	}
}
