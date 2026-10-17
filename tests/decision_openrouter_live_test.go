//go:build e2e && decision_live

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type liveDecisionCase struct {
	Model      string          `json:"model"`
	Case       string          `json:"case"`
	ExitCode   int             `json:"exitCode"`
	DurationMS int64           `json:"durationMs"`
	Response   json.RawMessage `json:"response,omitempty"`
	Usage      json.RawMessage `json:"usage,omitempty"`
	EventsPath string          `json:"eventsPath"`
	Error      string          `json:"error,omitempty"`
	Notes      []string        `json:"notes,omitempty"`
	Passed     bool            `json:"passed"`
}

func runDecisionLive(t *testing.T, work, name, source string) (liveDecisionCase, []map[string]json.RawMessage, string) {
	t.Helper()
	entry := liveDecisionCase{Case: name, EventsPath: filepath.Join(work, name+".jsonl")}
	file := filepath.Join(work, name+".js")
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliPath, "run", "--timeout=75s", "--events="+entry.EventsPath, file)
	cmd.Dir = repoRootPath
	cmd.Env = os.Environ()
	cmd.Env = envWith(cmd.Env, "NOE_AGENT_TRACE", "")
	cmd.Env = envWith(cmd.Env, "NOE_AGENT_RUNTIME_DIR", filepath.Join(work, "runtime"))
	var out, diagnostic bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	started := time.Now()
	err := cmd.Run()
	entry.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		entry.ExitCode = -1
		if cmd.ProcessState != nil {
			entry.ExitCode = cmd.ProcessState.ExitCode()
		}
		entry.Error = fmt.Sprintf("%v: %s", err, diagnostic.String())
	}
	os.WriteFile(filepath.Join(work, name+".stdout.txt"), out.Bytes(), 0600)
	os.WriteFile(filepath.Join(work, name+".stderr.txt"), diagnostic.Bytes(), 0600)
	var events []map[string]json.RawMessage
	if data, readErr := os.ReadFile(entry.EventsPath); readErr == nil {
		for _, line := range bytes.Split(data, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var event map[string]json.RawMessage
			if json.Unmarshal(line, &event) != nil {
				entry.Error += " invalid event JSON"
				continue
			}
			events = append(events, event)
		}
	}
	return entry, events, out.String()
}

func TestDecisionOpenRouterLive(t *testing.T) {
	if os.Getenv("NOE_OPENROUTER_API_KEY_FILE") == "" {
		t.Fatal("explicit NOE_OPENROUTER_API_KEY_FILE is required")
	}
	sdkVersion := cliDependencyVersion(t, "github.com/OpenRouterTeam/go-sdk")
	work := os.Getenv("NOE_DECISION_LIVE_DIR")
	if work == "" {
		t.Fatal("NOE_DECISION_LIVE_DIR is required to retain live evidence")
	}
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	var results []liveDecisionCase
	defer func() {
		raw, err := json.MarshalIndent(map[string]any{"verifiedAt": time.Now().UTC().Format(time.RFC3339), "sdk": sdkVersion, "results": results}, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(work, "results.json"), append(raw, '\n'), 0600); err != nil {
			t.Error(err)
		}
	}()
	commonQuestions := json.RawMessage(`{"payment":{"type":"noul","instructions":"Is this report about a payment or checkout problem?"},"team":{"type":"choice","instructions":"Which team should handle this report?","criteria":{"payments":"Payment or checkout issues","account":"Login or account issues","other":"Other issues"}},"severity":{"type":"score","instructions":"How severe is the described service disruption?","criteria":["No disruption","Some customers affected","All customers blocked"]}}`)
	structuredQuestions := json.RawMessage(`{"active":{"type":"noul","instructions":"Is this account active?"},"plan":{"type":"choice","instructions":"Which plan does this account have?","criteria":{"free":"Free plan","pro":"Pro plan"}}}`)
	imageQuestions := json.RawMessage(`{"color":{"type":"choice","instructions":"What is the dominant color in the image?","criteria":{"red":"Red","green":"Green","blue":"Blue"}},"red":{"type":"noul","instructions":"Does this image contain a red region?"}}`)
	imagePath, _ := json.Marshal(filepath.Join(repoRootPath, "tests/fixtures/decision/red-square.png"))
	for _, model := range []string{"typesafe/jev-1.14", "perplexity/pplx-decider-v1.2", "cloudflare/clef"} {
		for _, kind := range []string{"text", "structured", "image"} {
			t.Run(model+"/"+kind, func(t *testing.T) {
				var state json.RawMessage
				questions := commonQuestions
				prelude := ""
				switch kind {
				case "text":
					state = json.RawMessage(`"Checkout is broken for all customers. Nobody can finish a payment."`)
				case "structured":
					state = json.RawMessage(`{"name":"Ada","age":37,"account":{"active":true},"plan":"pro"}`)
					questions = structuredQuestions
				case "image":
					state = json.RawMessage(`[]`)
					questions = imageQuestions
					prelude = `const {readFile}=await import("node:fs/promises"); const {Buffer}=await import("node:buffer"); const image=Buffer.from(await readFile(` + string(imagePath) + `)).toString("base64");`
				}
				request, _ := json.Marshal(map[string]any{"provider": "openrouter", "model": model, "state": state, "questions": questions})
				source := `import {decide} from "noe:decision";` + prelude + `const request=` + string(request) + `;`
				if kind == "image" {
					source += `request.state=["Identify the image contents.",{type:"image_url",image_url:{url:"data:image/png;base64,"+image}}];`
				}
				source += `console.log(JSON.stringify(await decide(request)));`
				name := strings.ReplaceAll(model, "/", "-") + "-" + kind
				entry, events, out := runDecisionLive(t, work, name, source)
				entry.Model = model
				entry.Case = kind
				defer func() { results = append(results, entry) }()
				if entry.ExitCode != 0 || entry.Error != "" {
					t.Fatal(entry.Error)
				}
				if !json.Valid([]byte(strings.TrimSpace(out))) {
					entry.Error = "invalid response JSON"
					t.Fatal(entry.Error)
				}
				entry.Response = json.RawMessage(strings.TrimSpace(out))
				var response struct {
					Answers map[string]json.RawMessage `json:"answers"`
					Usage   json.RawMessage            `json:"usage"`
				}
				if json.Unmarshal(entry.Response, &response) != nil {
					entry.Error = "invalid response JSON"
					t.Fatal(entry.Error)
				}
				entry.Usage = response.Usage
				var expected map[string]struct {
					Type     string          `json:"type"`
					Criteria json.RawMessage `json:"criteria"`
				}
				json.Unmarshal(questions, &expected)
				if len(response.Answers) != len(expected) {
					entry.Error = "answer keys do not match questions"
					t.Fatal(entry.Error)
				}
				for key, q := range expected {
					var answer struct {
						Type          string             `json:"type"`
						Noul          *float64           `json:"noul"`
						Choice        string             `json:"choice"`
						Score         *float64           `json:"score"`
						Probabilities map[string]float64 `json:"probabilities"`
					}
					if json.Unmarshal(response.Answers[key], &answer) != nil || answer.Type != q.Type {
						entry.Error = "invalid typed answer " + key
						t.Fatal(entry.Error)
					}
					switch q.Type {
					case "noul":
						if answer.Noul == nil || !finiteRange(*answer.Noul, 0, 1) {
							entry.Error = "invalid noul " + key
						}
					case "choice":
						var choices map[string]any
						json.Unmarshal(q.Criteria, &choices)
						if _, ok := choices[answer.Choice]; !ok {
							entry.Error = "unknown choice " + key
						}
						if answer.Probabilities == nil {
							entry.Notes = append(entry.Notes, "choice probabilities not provided: "+key)
						}
						for label := range answer.Probabilities {
							if _, ok := choices[label]; !ok {
								entry.Error = "unknown probability choice " + key
							}
						}
					case "score":
						var levels []any
						json.Unmarshal(q.Criteria, &levels)
						if answer.Score == nil || !finiteRange(*answer.Score, 0, float64(len(levels)-1)) {
							entry.Error = "invalid score " + key
						}
					}
					for _, p := range answer.Probabilities {
						if !finiteRange(p, 0, 1) {
							entry.Error = "invalid probability " + key
						}
					}
					if entry.Error != "" {
						t.Fatal(entry.Error)
					}
				}
				started, completed := 0, 0
				for _, e := range events {
					switch string(e["type"]) {
					case `"decision.started"`:
						started++
					case `"decision.completed"`:
						completed++
					}
				}
				entry.Passed = started == 1 && completed == 1
				if !entry.Passed {
					entry.Error = "missing decision events"
					t.Fatal(entry.Error)
				}
				t.Logf("%s %s: %dms usage=%s", model, kind, entry.DurationMS, entry.Usage)
			})
		}
	}
	t.Run("parent-agent", func(t *testing.T) {
		instruction := `Use run_js and import {decide} from "noe:decision". Call decide once with provider:"openrouter", model:"typesafe/jev-1.14", state:"Checkout fails after payment", and questions:{team:{type:"choice",instructions:"Which team owns the issue?",criteria:{payments:"Payment or checkout issues",account:"Login or account issues"}}}. Print the decision result and use its choice to report which team should handle the issue. Do not read files or run shell commands. Return a short final answer.`
		encoded, _ := json.Marshal(instruction)
		source := `import {run_agent} from "noe:agent"; const r=await run_agent({provider:"openrouter",model:"deepseek/deepseek-v4.1-flash",effort:"auto",maxSteps:4,maxTokens:4096},` + string(encoded) + `);if(r.lastResult.state!=="completed")throw Error(JSON.stringify(r));console.log(r.lastResult.answer);`
		entry, events, _ := runDecisionLive(t, work, "parent-agent", source)
		entry.Model = "deepseek/deepseek-v4.1-flash -> typesafe/jev-1.14"
		defer func() { results = append(results, entry) }()
		if entry.ExitCode != 0 || entry.Error != "" {
			t.Fatal(entry.Error)
		}
		count := 0
		for _, e := range events {
			if string(e["type"]) == `"decision.completed"` {
				count++
				if string(e["decisionModel"]) != `"typesafe/jev-1.14"` || string(e["model"]) != `"deepseek/deepseek-v4.1-flash"` || len(e["runId"]) == 0 || len(e["toolCallId"]) == 0 {
					entry.Error = "missing parent/decision metadata"
					t.Fatal(entry.Error)
				}
			}
		}
		entry.Passed = count == 1
		if !entry.Passed {
			entry.Error = fmt.Sprintf("expected one decision, got %d", count)
			t.Fatal(entry.Error)
		}
	})
}

func finiteRange(x, low, high float64) bool {
	return !math.IsNaN(x) && !math.IsInf(x, 0) && x >= low && x <= high
}
