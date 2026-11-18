//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDocsSearchInRunJS(t *testing.T) {
	var calls atomic.Int32
	provider := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		switch calls.Add(1) {
		case 1:
			call := agentTool("search-docs", `import {docs, searchDocs} from "noe:docs";
const [match] = searchDocs("read part of a file");
export default {match, reference: docs(match.topic, match.member)};`)
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{call}}}, FinishReason: "tool_calls"}
		case 2:
			if len(req.Messages) == 0 || req.Messages[len(req.Messages)-1].ToolCallID != "search-docs" {
				t.Error("missing run_js search result")
				return agentModelReply{Status: 500}
			}
			result := agentToolResult(t, req.Messages[len(req.Messages)-1])
			var data struct {
				Match     struct{ Topic, Member, Excerpt string }
				Reference string
			}
			if err := json.Unmarshal(result.Data, &data); err != nil || result.Reason != "completed" ||
				data.Match.Topic != "noe:fs" || data.Match.Member != "FileHandle.readAt" ||
				!strings.Contains(data.Match.Excerpt, "Promise<Uint8Array>") ||
				!strings.Contains(data.Reference, "bytes.byteLength") {
				t.Errorf("run_js search result = %+v, error = %v", result, err)
			}
			return agentModelReply{Content: "found the positional file read"}
		default:
			t.Error("unexpected provider request")
			return agentModelReply{Status: 500}
		}
	})
	defer provider.Close()
	env := agentCLIEnv(t, provider.URL)
	env["NOE_AGENT_TRACE"] = ""
	result := runCLI(t, t.TempDir(), env, "eval", "--no-inst", agentScript("Find a way to read part of a file", "{maxSteps:2}"))
	requireExit(t, result, 0)
	if result.stdout != "found the positional file read\n" || calls.Load() != 2 {
		t.Fatalf("agent result = %+v, provider calls = %d", result, calls.Load())
	}
}

func TestDocsStandaloneBinaryAndManagedEval(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, filepath.Base(cliPath))
	src, err := os.Open(cliPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(binary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy binary: %v, close: %v", copyErr, closeErr)
	}
	writeFixture(t, filepath.Join(dir, "invalid.json"), "invalid configuration")
	writeFixture(t, filepath.Join(dir, "config.json"), "{}")
	t.Setenv("NOE_CONFIG_FILE", filepath.Join(dir, "config.json"))
	for _, tc := range []struct{ topic, member string }{{"noe:sockets", "connect"}, {"globals", "fetch"}, {"noe:http", "serve"}, {"noe:agent", "hooks"}, {"noe:agent", "TaskOutcome.hookFailures"}} {
		t.Run(tc.topic+"/"+tc.member, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "docs", tc.topic, tc.member)
			cmd.Dir = dir
			cmd.Env = envWith(os.Environ(), "NOE_CONFIG_FILE", filepath.Join(dir, "invalid.json"))
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil || stderr.Len() != 0 {
				t.Fatalf("standalone docs: %v stderr=%s", err, &stderr)
			}
			if !strings.Contains(stdout.String(), "## "+tc.member) {
				t.Fatal("missing API reference detail")
			}
			if tc.topic == "noe:agent" {
				if strings.Contains(stdout.String(), "Hook exceptions fail the task") {
					t.Fatal("obsolete hook failure contract")
				}
				required := []string{"does not skip the original tool", "scope/Host failures"}
				if tc.member == "TaskOutcome.hookFailures" {
					required = []string{"notifiedRequestId", "includeAnswer:false", "force an extra request"}
				}
				for _, text := range required {
					if !strings.Contains(stdout.String(), text) {
						t.Errorf("agent detail missing %q", text)
					}
				}
			}
			code := fmt.Sprintf("// Read the bundled API contract.\nconst {docs}=await import(\"noe:docs\");console.log(docs(%q,%q));", tc.topic, tc.member)
			result := runCLIExecutable(t, binary, dir, nil, strings.NewReader(""), "eval", code)
			requireExit(t, result, 0)
			if result.stderr != "" || strings.TrimSpace(result.stdout) != strings.TrimSpace(stdout.String()) {
				t.Fatalf("standalone CLI/eval reference differs: %+v", result)
			}
		})
	}
}
