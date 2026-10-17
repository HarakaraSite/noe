//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type mcpProcessSummary struct {
	Route             string `json:"route"`
	SDK               string `json:"sdk"`
	Initialized       bool   `json:"initialized"`
	Tools             int    `json:"tools"`
	ConcurrentCalls   int    `json:"concurrentCalls"`
	LargeInputBytes   int    `json:"largeInputBytes"`
	StartupFailure    bool   `json:"startupFailure"`
	EOFRejectsPending bool   `json:"eofRejectsPendingRPC"`
	BadRejectsPending bool   `json:"malformedRejectsPendingRPC"`
	ChildCleanup      bool   `json:"childCleanup"`
	StartupCloseRace  bool   `json:"startupCloseRace"`
	PendingWriteClose bool   `json:"pendingWriteClose"`
}

func TestImportMapMCPProcessIO(t *testing.T) {
	bundle := os.Getenv("NOE_MCP_SDK_BUNDLE")
	if bundle == "" {
		t.Skip("set NOE_MCP_SDK_BUNDLE to a prepared @modelcontextprotocol/sdk 1.32.1 bundle")
	}
	if !filepath.IsAbs(bundle) {
		t.Fatalf("NOE_MCP_SDK_BUNDLE must be absolute: %q", bundle)
	}
	bundleInfo, err := os.Stat(bundle)
	if err != nil || !bundleInfo.Mode().IsRegular() {
		t.Fatalf("SDK bundle %q is unavailable or not a regular file: %v", bundle, err)
	}
	if filepath.Ext(bundle) != ".mjs" {
		t.Fatalf("SDK bundle must have .mjs extension: %q", bundle)
	}

	spike := filepath.Join(repoRootPath, "scripts", "spikes", "process-io-mcp")
	server := filepath.Join(spike, "server.js")
	transport := filepath.Join(spike, "noe-transport.js")
	exercise := filepath.Join(spike, "consumer", "exercise.js")
	work := t.TempDir()
	importMapPath := filepath.Join(work, "import-map.json")
	importMap, err := json.Marshal(map[string]any{"imports": map[string]string{
		"mcp/client":    bundle,
		"mcp/transport": transport,
		"mcp/exercise":  exercise,
	}})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, importMapPath, string(importMap))

	config := map[string]any{
		"command": cliPath,
		"args":    []string{"run", "--timeout", "20s", server},
		"cwd":     work,
	}
	encodedConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ name, entry string }{
		{"main", filepath.Join(spike, "consumer", "main.js")},
		{"worker", filepath.Join(spike, "consumer", "worker-main.js")},
	} {
		t.Run(route.name, func(t *testing.T) {
			args := []string{"run", "--import-map", importMapPath, route.entry, string(encodedConfig)}
			if route.name == "worker" {
				args = append(args, filepath.Join(spike, "consumer", "worker.js"))
			}
			result := runCLI(t, work, nil, args...)
			requireExit(t, result, 0)
			if result.stderr != "" {
				t.Fatalf("stderr=%q", result.stderr)
			}
			assertMCPProcessSummary(t, result.stdout, route.name)
		})
	}

	failureConfig := map[string]string{"command": cliPath, "cwd": work, "server": server}
	encodedFailureConfig, err := json.Marshal(failureConfig)
	if err != nil {
		t.Fatal(err)
	}
	failures := runCLI(t, work, nil, "run", "--import-map", importMapPath,
		filepath.Join(spike, "consumer", "failures.js"), string(encodedFailureConfig))
	requireExit(t, failures, 0)
	if failures.stderr != "" {
		t.Fatalf("failure checks stderr=%q", failures.stderr)
	}
	var failureSummary mcpProcessSummary
	if err := json.Unmarshal([]byte(strings.TrimSpace(failures.stdout)), &failureSummary); err != nil {
		t.Fatalf("failure summary %q: %v", failures.stdout, err)
	}
	if !failureSummary.StartupFailure || !failureSummary.EOFRejectsPending || !failureSummary.BadRejectsPending || !failureSummary.ChildCleanup || !failureSummary.StartupCloseRace || !failureSummary.PendingWriteClose {
		t.Fatalf("failure summary=%+v", failureSummary)
	}

	var calls atomic.Int32
	provider := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		switch calls.Add(1) {
		case 1:
			if agentUser(req) != "exercise local MCP through run_js" {
				t.Errorf("unexpected agent instruction: %q", agentUser(req))
			}
			code := `// Exercise local MCP through the inherited import-map snapshot.
const {exercise}=await import("mcp/exercise");const result=await exercise(` + string(encodedConfig) + `);console.log(JSON.stringify({route:"agent",...result}));`
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("local-mcp", code)}}}, FinishReason: "tool_calls"}
		case 2:
			if len(req.Messages) == 0 {
				t.Error("agent follow-up omitted run_js result")
				return agentModelReply{Status: 500}
			}
			tool := agentToolResult(t, req.Messages[len(req.Messages)-1])
			if tool.ExitCode != 0 || tool.Stderr != "" {
				t.Errorf("agent run_js result=%+v", tool)
				return agentModelReply{Content: "run_js failed"}
			}
			assertMCPProcessSummary(t, tool.Stdout, "agent")
			return agentModelReply{Content: "agent route succeeded"}
		default:
			t.Errorf("unexpected provider request #%d", calls.Load())
			return agentModelReply{Status: 500}
		}
	})
	defer provider.Close()
	env := agentCLIEnv(t, provider.URL)
	env["NOE_AGENT_TRACE"] = ""
	agentSource := `import {run_agent} from "noe:agent";const result=await run_agent({provider:"openrouter",model:"fixture/model",maxSteps:2,effort:"auto"},"exercise local MCP through run_js");console.log(result.lastResult.answer);`
	// Run the root consumer as an actual agent task so the local provider's
	// returned run_js source inherits the same immutable import-map snapshot.
	agentResult := runCLI(t, work, env, "eval", "--import-map", importMapPath, agentSource)
	requireExit(t, agentResult, 0)
	if agentResult.stderr != "" || agentResult.stdout != "agent route succeeded\n" || calls.Load() != 2 {
		t.Fatalf("agent result=%+v provider calls=%d", agentResult, calls.Load())
	}
}

func assertMCPProcessSummary(t *testing.T, output, route string) {
	t.Helper()
	var summary mcpProcessSummary
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &summary); err != nil {
		t.Fatalf("%s summary %q: %v", route, output, err)
	}
	if summary.Route != route || summary.SDK != "1.32.1" || !summary.Initialized || summary.Tools != 1 || summary.ConcurrentCalls != 3 || summary.LargeInputBytes != 270000 {
		t.Fatalf("%s summary=%+v", route, summary)
	}
}
