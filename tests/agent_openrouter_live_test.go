//go:build e2e && agent_live

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This explicitly tagged acceptance test spends OpenRouter credit. Ordinary
// unit/E2E/race commands cannot select it. The Go host alone reads the key file.
func TestAgentOpenRouterLive(t *testing.T) {
	keyFile := os.Getenv("NOE_OPENROUTER_API_KEY_FILE")
	if keyFile == "" {
		t.Fatal("explicit NOE_OPENROUTER_API_KEY_FILE is required for live acceptance")
	}
	sdkVersion := cliDependencyVersion(t, "github.com/OpenRouterTeam/go-sdk")
	type evidence struct {
		Name         string            `json:"name"`
		Source       string            `json:"source"`
		Command      []string          `json:"command"`
		ExitCode     int               `json:"exitCode"`
		DurationMS   int64             `json:"durationMs"`
		Stdout       string            `json:"stdout"`
		Trace        []json.RawMessage `json:"trace"`
		Diagnostic   string            `json:"diagnostic,omitempty"`
		ModelCalls   int               `json:"modelCalls"`
		JSExecutions int               `json:"jsExecutions"`
		CachedTokens int64             `json:"cachedTokens"`
		Cost         float64           `json:"cost"`
		Passed       bool              `json:"passed"`
	}
	var results []evidence
	defer func() {
		report := struct {
			VerifiedAt string     `json:"verifiedAt"`
			Platform   string     `json:"platform"`
			GoVersion  string     `json:"goVersion"`
			Model      string     `json:"model"`
			SDK        string     `json:"sdk"`
			Results    []evidence `json:"results"`
		}{time.Now().UTC().Format(time.RFC3339), runtime.GOOS + "/" + runtime.GOARCH, runtime.Version(), "deepseek/deepseek-v4.1-flash", sdkVersion, results}
		if path := os.Getenv("NOE_AGENT_LIVE_RESULTS"); path != "" {
			data, err := json.MarshalIndent(report, "", "  ")
			if err == nil {
				err = os.WriteFile(path, append(data, '\n'), 0600)
			}
			if err != nil {
				t.Error(err)
			}
		}
	}()
	work := filepath.Join(repoRootPath, "tests", "fixtures", "agent-live")
	for _, name := range []string{"aggregate", "repair", "parallel"} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(work, name+".js")
			entry := evidence{Name: name, Source: filepath.ToSlash(filepath.Join("tests", "fixtures", "agent-live", name+".js")), Command: []string{"noe", "run", "--timeout", "120s", file}, ExitCode: -1}
			defer func() { entry.Passed = !t.Failed(); results = append(results, entry) }()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, cliPath, "run", "--timeout", "120s", file)
			cmd.Dir = work
			for _, pair := range os.Environ() {
				key, _, _ := strings.Cut(pair, "=")
				if key == "NOE_AGENT_CONNECTION" || key == "NOE_OPENROUTER_BASE_URL" || key == "NOE_OPENROUTER_API_KEY_FILE" || key == "NOE_AGENT_TRACE" {
					continue
				}
				cmd.Env = append(cmd.Env, pair)
			}
			cmd.Env = append(cmd.Env, "NOE_OPENROUTER_API_KEY_FILE="+keyFile, "NOE_AGENT_TRACE=1")
			var out, diagnostic bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &diagnostic
			started := time.Now()
			err := cmd.Run()
			entry.DurationMS = time.Since(started).Milliseconds()
			entry.Stdout = out.String()
			if cmd.ProcessState != nil {
				entry.ExitCode = cmd.ProcessState.ExitCode()
			}
			var records []struct {
				RunID, ParentRunID, Name, Status, Reason string
				Usage                                    struct {
					CachedTokens int64
					Cost         float64
				}
			}
			for _, line := range strings.Split(diagnostic.String(), "\n") {
				if strings.HasPrefix(line, "{") && json.Valid([]byte(line)) {
					entry.Trace = append(entry.Trace, json.RawMessage(line))
					var record struct {
						RunID, ParentRunID, Name, Status, Reason string
						Usage                                    struct {
							CachedTokens int64
							Cost         float64
						}
					}
					json.Unmarshal([]byte(line), &record)
					records = append(records, record)
					if record.Status == "model_response" {
						entry.ModelCalls++
						entry.CachedTokens += record.Usage.CachedTokens
						entry.Cost += record.Usage.Cost
					}
					if record.Status == "js_result" {
						entry.JSExecutions++
					}
				} else if strings.TrimSpace(line) != "" {
					entry.Diagnostic += line + "\n"
				}
			}
			if err != nil || entry.ExitCode != 0 {
				t.Fatalf("live %s exit=%d error=%v diagnostic=%s", name, entry.ExitCode, err, entry.Diagnostic)
			}
			// Parse only for acceptance; the runtime's final string is recorded
			// above exactly as returned, with no JSON/Markdown normalization.
			text := strings.TrimSpace(entry.Stdout)
			first, last := strings.Index(text, "{"), strings.LastIndex(text, "}")
			if first < 0 || last < first {
				t.Fatalf("no result object: %s", text)
			}
			var values map[string]float64
			if err := json.Unmarshal([]byte(text[first:last+1]), &values); err != nil {
				t.Fatal(err)
			}
			want := map[string]float64{"count": 4, "total": 100, "average": 25}
			if name == "parallel" {
				want = map[string]float64{"left": 30, "right": 70, "total": 100}
			}
			for key, value := range want {
				if got, ok := values[key]; !ok || got != value {
					t.Errorf("%s=%v (present=%v), want %v; output=%s", key, got, ok, value, text)
				}
			}
			if entry.JSExecutions == 0 {
				t.Error("no actual JS execution in trace")
			}
			if name == "repair" {
				failed, modelAfterFailure, repaired := false, false, false
				for _, record := range records {
					if record.Status == "js_result" && record.Reason == "failed" {
						failed = true
					}
					if failed && record.Status == "model_response" {
						modelAfterFailure = true
					}
					if modelAfterFailure && record.Status == "js_result" && record.Reason == "completed" {
						repaired = true
					}
				}
				if !repaired {
					t.Error("missing failed JS -> model correction -> successful JS sequence")
				}
			}
			if name == "parallel" {
				parents := map[string]string{}
				jsRuns := map[string]bool{}
				names := map[string]bool{}
				for _, record := range records {
					if record.Status == "started" {
						parents[record.RunID] = record.ParentRunID
						names[record.Name] = true
					}
					if record.Status == "js_result" && record.Reason == "completed" {
						jsRuns[record.RunID] = true
					}
				}
				root := ""
				children := 0
				for id, parent := range parents {
					if parent == "" {
						root = id
					}
				}
				for id, parent := range parents {
					if parent == root && id != root {
						children++
						if !jsRuns[id] {
							t.Error("child did not execute JS")
						}
					}
				}
				if root == "" || children != 2 || !names["left"] || !names["right"] || !jsRuns[root] {
					t.Errorf("missing parent+2 child JS hierarchy: %v %v", parents, names)
				}
			}
			t.Logf("%s: output=%s models=%d js=%d cached=%d cost=%g duration=%dms", name, text, entry.ModelCalls, entry.JSExecutions, entry.CachedTokens, entry.Cost, entry.DurationMS)
		})
	}
}
