//go:build e2e && docs_live

package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDocsOpenCodeLive gives normal tasks without naming the reference system.
// Only the built-in initial instructions introduce how to discover API contracts.
func TestDocsOpenCodeLive(t *testing.T) {
	if os.Getenv("NOE_AGENT_DOCS_LIVE") != "1" {
		t.Skip("set NOE_AGENT_DOCS_LIVE=1 to send real provider requests")
	}
	root := os.Getenv("NOE_AGENT_DOCS_LIVE_DIR")
	if root == "" {
		t.Fatal("NOE_AGENT_DOCS_LIVE_DIR required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	key := os.Getenv("NOE_OPENCODE_GO_API_KEY_FILE")
	if key == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		key = filepath.Join(config, "noe", "credentials", "opencode-go-api-key")
	}
	key, err = filepath.Abs(key)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(key); err != nil || !info.Mode().IsRegular() {
		t.Fatal("OpenCode Go credential file unavailable")
	}
	for _, tc := range []struct{ name, instruction string }{
		{"positional_file", "In this temporary workspace, update the existing data.bin in place: overwrite the four bytes starting at byte offset 5 with ASCII WXYZ. Use positional file I/O; do not read and rewrite the whole file. Read exactly 8 bytes starting at offset 3 before and after the patch. Create report.json with string fields before and after containing those two windows. Preserve the file length and every byte outside the patch. Release the handle. Perform the work in JavaScript without external commands, network, or delegation. Briefly report the result."},
		{"tcp_half_close", "Write and execute a JavaScript program that runs a TCP server and client together on 127.0.0.1 with an OS-assigned port. The client sends UTF-8 ping, then ends only its sending direction. The server must read until that EOF, then reply with UTF-8 ACK:ping and end its sending direction. The client must read the complete reply. Save report.json as {request:\"ping\",response:\"ACK:ping\",port:NUMBER}, using the actual listening port. Close both connections and the listener, including on failure. Use raw TCP byte streams, no HTTP, external commands, or delegation. Briefly report the result."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(strings.ToLower(tc.instruction), "docs") {
				t.Fatal("task must not prompt reference use")
			}
			dir := filepath.Join(root, tc.name)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatalf("use a fresh artifact directory for each trial: %v", err)
			}
			work := filepath.Join(dir, "workspace")
			if err := os.Mkdir(work, 0700); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, filepath.Join(work, "data.bin"), "0123456789abcdefghijklmnop")
			writeFixture(t, filepath.Join(dir, "config.json"), "{}")
			writeFixture(t, filepath.Join(dir, "instruction.txt"), tc.instruction)
			source := fmt.Sprintf(`import {run_agent} from "noe:agent";
const result=await run_agent({provider:"opencode-go",model:"deepseek-v4.1-flash",effort:"auto",name:%q,maxTokens:8192,maxSteps:10,executionLimits:{providerCalls:10}},%q);
console.log(JSON.stringify(result));
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result.lastResult));`, tc.name, tc.instruction)
			writeFixture(t, filepath.Join(dir, "task.mjs"), source)
			ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, cliPath, "run", "--timeout=175s", "--events="+filepath.Join(dir, "events.jsonl"), filepath.Join(dir, "task.mjs"))
			cmd.Dir = work
			// Isolate optional user instructions while keeping the explicitly
			// configured credential file outside the experiment workspace.
			cmd.Env = envWith(os.Environ(), "XDG_CONFIG_HOME", filepath.Join(dir, "config-home"))
			cmd.Env = envWith(cmd.Env, "NOE_CONFIG_FILE", filepath.Join(dir, "config.json"))
			cmd.Env = envWith(cmd.Env, "NOE_OPENCODE_GO_API_KEY_FILE", key)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			started := time.Now()
			runErr := cmd.Run()
			writeFixture(t, filepath.Join(dir, "stdout.txt"), stdout.String())
			writeFixture(t, filepath.Join(dir, "stderr.txt"), stderr.String())
			lookups, toolCalls, providerCalls := inspectDocsLiveEvents(t, dir)
			summary := map[string]any{"provider": "opencode-go", "model": "deepseek-v4.1-flash", "taskMentionsDocs": false, "elapsedMs": time.Since(started).Milliseconds(), "commandSuccess": runErr == nil, "successfulReferenceCalls": lookups, "toolCalls": toolCalls, "providerCalls": providerCalls}
			defer func() {
				summary["validationPassed"] = !t.Failed()
				data, _ := json.MarshalIndent(summary, "", "  ")
				writeFixture(t, filepath.Join(dir, "summary.json"), string(data)+"\n")
			}()
			t.Logf("artifacts=%s requests=%d toolCalls=%d successfulReferenceCalls=%d elapsed=%s", dir, providerCalls, toolCalls, lookups, time.Since(started).Round(time.Second))
			if runErr != nil {
				t.Errorf("live task failed: %v; see saved stdout/stderr", runErr)
			}
			if lookups == 0 {
				t.Error("agent did not successfully consult bundled reference from the initial instructions")
			}
			data, err := os.ReadFile(filepath.Join(work, "report.json"))
			if err != nil {
				t.Fatalf("missing task artifact: %v", err)
			}
			if tc.name == "positional_file" {
				var report struct{ Before, After string }
				if err := json.Unmarshal(data, &report); err != nil || report.Before != "3456789a" || report.After != "34WXYZ9a" {
					t.Errorf("incorrect byte windows: %s (error %v)", data, err)
				}
				got, err := os.ReadFile(filepath.Join(work, "data.bin"))
				if err != nil || string(got) != "01234WXYZ9abcdefghijklmnop" {
					t.Errorf("file content not preserved: %q (error %v)", got, err)
				}
			} else {
				var report struct {
					Request, Response string
					Port              int
				}
				if err := json.Unmarshal(data, &report); err != nil || report.Request != "ping" || report.Response != "ACK:ping" || report.Port < 1 || report.Port > 65535 {
					t.Errorf("incorrect TCP exchange: %s (error %v)", data, err)
				}
			}
		})
	}
}

func inspectDocsLiveEvents(t *testing.T, dir string) (lookups, tools, requests int) {
	t.Helper()
	file, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 65536), 16*1024*1024)
	for scanner.Scan() {
		var event struct {
			Type      string                `json:"type"`
			Arguments struct{ Code string } `json:"arguments"`
			RawResult struct {
				Stdout   string `json:"stdout"`
				ExitCode int    `json:"exitCode"`
			} `json:"rawResult"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "model.started" {
			requests++
		}
		if event.Type != "tool.finished" {
			continue
		}
		tools++
		if event.RawResult.ExitCode == 0 && strings.Contains(event.Arguments.Code, "noe:docs") && strings.Contains(event.RawResult.Stdout, "# ") {
			lookups++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return
}
