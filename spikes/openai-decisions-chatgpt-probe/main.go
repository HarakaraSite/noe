// Probe Decisions with explicitly selected ChatGPT OAuth or API-key authentication; never fall back.
package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
)

func probe(useAPIKey bool) map[string]any {
	const endpoint = "https://api.openai.com/v1/decisions"
	report := map[string]any{
		"checkedAt":        time.Now().UTC().Format(time.RFC3339),
		"endpoint":         endpoint,
		"authentication":   "existing ChatGPT OAuth access token",
		"model":            "gpt-6-luna",
		"decisionRequests": 0,
		"apiKeyFallback":   false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := &http.Client{
		Timeout:       45 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	envName, defaultName := "NOE_OPENAI_CHATGPT_CREDENTIALS_FILE", "openai-chatgpt.json"
	if useAPIKey {
		envName, defaultName = "NOE_OPENAI_API_KEY_FILE", "openai-api-key"
		report["authentication"] = "explicit OpenAI API key"
	}
	file := os.Getenv(envName)
	if file == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			report["stage"] = "credential-path"
			report["error"] = err.Error()
			return report
		}
		file = filepath.Join(base, "noe", "credentials", defaultName)
	}
	var token string
	var err error
	if useAPIKey {
		var key []byte
		key, err = os.ReadFile(file)
		token = strings.TrimSpace(string(key))
	} else {
		token, err = credentials.NewChatGPT(file, credentials.Options{HTTPClient: client}).Token(ctx)
	}
	if err != nil {
		report["stage"] = "credential"
		report["error"] = err.Error()
		return report
	}
	if token == "" {
		report["stage"] = "credential"
		report["error"] = "Selected credential is empty"
		return report
	}
	redact := func(value string) string {
		value = strings.ReplaceAll(value, token, "[REDACTED]")
		if strings.Contains(value, "sk-") {
			return "[REDACTED credential-bearing message]"
		}
		return value
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader("{\"model\":\"gpt-6-luna\",\"input\":\"The flag is true.\",\"questions\":[{\"type\":\"predicate\",\"name\":\"flag_true\",\"instructions\":\"Is the flag true?\"}]}"))
	if err != nil {
		report["stage"] = "request"
		report["error"] = "Could not create request"
		return report
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	started := time.Now()
	report["decisionRequests"] = 1
	response, err := client.Do(request)
	report["durationMs"] = time.Since(started).Milliseconds()
	if err != nil {
		report["stage"] = "transport"
		report["error"] = redact(err.Error())
		return report
	}
	defer response.Body.Close()
	report["httpStatus"] = response.StatusCode
	report["requestId"] = response.Header.Get("x-request-id")
	data, err := io.ReadAll(response.Body)
	if err != nil {
		report["stage"] = "response-read"
		report["error"] = redact(err.Error())
		return report
	}
	var payload map[string]any
	if json.Unmarshal(data, &payload) != nil {
		report["stage"] = "response-json"
		report["error"] = "Response was not a JSON object; body not logged"
		return report
	}
	report["stage"] = "response"
	for _, key := range []string{"model", "answers", "usage"} {
		if value, ok := payload[key]; ok {
			report[key] = value
		}
	}
	if failure, ok := payload["error"].(map[string]any); ok {
		selected := map[string]any{}
		for _, key := range []string{"type", "code", "param", "message"} {
			if value, ok := failure[key]; ok {
				if text, ok := value.(string); ok {
					value = redact(text)
				}
				selected[key] = value
			}
		}
		report["apiError"] = selected
	}
	answers, _ := payload["answers"].([]any)
	validAnswer := false
	if len(answers) == 1 {
		answer, _ := answers[0].(map[string]any)
		probability, ok := answer["probability"].(float64)
		validAnswer = ok && probability >= 0 && probability <= 1 && answer["type"] == "predicate" && answer["name"] == "flag_true"
	}
	report["authenticatedDecisionObserved"] = response.StatusCode == http.StatusOK && validAnswer
	return report
}

func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if encoder.Encode(probe(len(os.Args) == 2 && os.Args[1] == "--api-key")) != nil {
		os.Exit(1)
	}
}
