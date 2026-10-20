//go:build e2e && hooks_live

package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	hooksLiveOptIn = "NOE_AGENT_HOOKS_LIVE"
	hooksLiveDir   = "NOE_AGENT_HOOKS_LIVE_DIR"
	hooksLiveModel = "deepseek-v4.1-flash"
)

// TestHooksOpenCodeLive is deliberately excluded from ordinary E2E builds.
// It uses the one explicitly authorized provider through a local metadata-only
// proxy; request and response bodies are parsed transiently and never saved.
func TestHooksOpenCodeLive(t *testing.T) {
	if os.Getenv(hooksLiveOptIn) != "1" {
		t.Skipf("set %s=1 to run authorized OpenCode live acceptance", hooksLiveOptIn)
	}
	artifactRoot := os.Getenv(hooksLiveDir)
	if strings.TrimSpace(artifactRoot) == "" {
		t.Fatal("explicit NOE_AGENT_HOOKS_LIVE_DIR is required for live artifacts")
	}
	artifactRoot, err := filepath.Abs(artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(artifactRoot); err != nil || !info.IsDir() {
		t.Fatalf("live artifact directory must already exist: %v", err)
	}
	keyFile := os.Getenv("NOE_OPENCODE_GO_API_KEY_FILE")
	if keyFile == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		keyFile = filepath.Join(configDir, "noe", "credentials", "opencode-go-api-key")
	}
	if info, err := os.Stat(keyFile); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("authorized opencode-go credential file is unavailable: %v", err)
	}
	keyFile, err = filepath.Abs(keyFile)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("L1_notice_and_tool_roundtrip", func(t *testing.T) { runHooksLiveL1(t, artifactRoot, keyFile) })
	t.Run("L2_owned_descendants_and_queue", func(t *testing.T) { runHooksLiveL2(t, artifactRoot, keyFile) })
	t.Run("L3_steer_during_hook", func(t *testing.T) { runHooksLiveSteer(t, artifactRoot, keyFile) })
	t.Run("L3_cancel_stalled_hook", func(t *testing.T) { runHooksLiveHookCancel(t, artifactRoot, keyFile) })
	t.Run("L3_cancel_run_js_process_tree", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("PID tree verification uses Linux process identity")
		}
		runHooksLiveToolCancel(t, artifactRoot, keyFile)
	})
	t.Run("L4_shared_limit_isolation", func(t *testing.T) { runHooksLiveL4(t, artifactRoot, keyFile) })
}

type hooksLiveToolRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// hooksLiveWireReceipt is an allowlist of request/response facts. It never
// contains request bodies, response text, Authorization, or arbitrary headers.
type hooksLiveWireReceipt struct {
	Sequence             int                `json:"sequence"`
	Method               string             `json:"method"`
	Path                 string             `json:"path"`
	Status               int                `json:"status,omitempty"`
	UserAgent            string             `json:"userAgent,omitempty"`
	SessionID            string             `json:"sessionId,omitempty"`
	Model                string             `json:"model,omitempty"`
	MessageCount         int                `json:"messageCount"`
	RoleCounts           map[string]int     `json:"roleCounts"`
	ContextNoticeCount   int                `json:"contextNoticeCount"`
	ContextNoticeAtTail  bool               `json:"contextNoticeAtTail"`
	SteerMarkerPresent   bool               `json:"steerMarkerPresent"`
	AssistantReasoning   bool               `json:"assistantReasoning"`
	ToolCalls            []hooksLiveToolRef `json:"toolCalls,omitempty"`
	ToolResults          []string           `json:"toolResults,omitempty"`
	ResponseHasReasoning bool               `json:"responseHasReasoning"`
	Rejected             string             `json:"rejected,omitempty"`
}

type hooksLiveMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tool_calls"`
	ReasoningContent json.RawMessage `json:"reasoning_content"`
	ReasoningDetails json.RawMessage `json:"reasoning_details"`
	Reasoning        json.RawMessage `json:"reasoning"`
}

type hooksLiveWireRequest struct {
	Model    string             `json:"model"`
	Messages []hooksLiveMessage `json:"messages"`
}

type hooksLiveRecorder struct {
	mu       sync.Mutex
	receipts []*hooksLiveWireReceipt
	byID     map[int]*hooksLiveWireReceipt
}

type hooksLiveWireKey struct{}

type hooksLiveProxy struct {
	server   *http.Server
	listener net.Listener
	url      string
	recorder *hooksLiveRecorder
}

func newHooksLiveProxy(t *testing.T) *hooksLiveProxy {
	t.Helper()
	target, err := url.Parse("https://opencode.ai/inference/go/openai/v1")
	if err != nil {
		t.Fatal(err)
	}
	recorder := &hooksLiveRecorder{byID: make(map[int]*hooksLiveWireReceipt)}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		// OpenCode sees its own hostname even though the test connects to loopback.
		request.Host = target.Host
	}
	proxy.ErrorLog = logDiscard()
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "authorized upstream unavailable", http.StatusBadGateway)
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		id := 0
		if response.Request != nil {
			id, _ = response.Request.Context().Value(hooksLiveWireKey{}).(int)
		}
		recorder.response(id, response.StatusCode)
		if response.Body != nil {
			upstreamBody := response.Body
			response.Body = hooksLiveReadCloser{
				Reader: io.TeeReader(upstreamBody, &hooksLiveReasoningTap{recorder: recorder, id: id}),
				Closer: upstreamBody,
			}
		}
		return nil
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, readErr := io.ReadAll(request.Body)
		_ = request.Body.Close()
		if readErr != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		var parsed hooksLiveWireRequest
		if json.Unmarshal(body, &parsed) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		receipt := inspectHooksLiveRequest(request, parsed)
		id := recorder.add(receipt)
		if request.Method != http.MethodPost {
			recorder.reject(id, "method")
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		if request.URL.Path != "/chat/completions" {
			recorder.reject(id, "route")
			http.Error(w, "unexpected provider route", http.StatusNotFound)
			return
		}
		if parsed.Model != hooksLiveModel {
			recorder.reject(id, "model")
			http.Error(w, "unexpected provider model", http.StatusBadRequest)
			return
		}
		ctx := context.WithValue(request.Context(), hooksLiveWireKey{}, id)
		proxy.ServeHTTP(w, request.Clone(ctx))
	})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	return &hooksLiveProxy{server: server, listener: listener, url: "http://" + listener.Addr().String(), recorder: recorder}
}

func logDiscard() *log.Logger { return log.New(io.Discard, "", 0) }

func inspectHooksLiveRequest(request *http.Request, parsed hooksLiveWireRequest) *hooksLiveWireReceipt {
	receipt := &hooksLiveWireReceipt{
		Method: request.Method, Path: request.URL.Path, UserAgent: request.Header.Get("User-Agent"),
		SessionID: request.Header.Get("x-opencode-session"), Model: parsed.Model,
		MessageCount: len(parsed.Messages), RoleCounts: make(map[string]int),
	}
	for index, message := range parsed.Messages {
		receipt.RoleCounts[message.Role]++
		if message.Role == "user" && hasContextNotice(message.Content) {
			receipt.ContextNoticeCount++
			if index == len(parsed.Messages)-1 {
				receipt.ContextNoticeAtTail = true
			}
		}
		if message.Role == "user" && bytes.Contains(message.Content, []byte("NOE_LIVE_STEER_MARKER_20261018")) {
			receipt.SteerMarkerPresent = true
		}
		if message.Role == "assistant" && (hasJSONValue(message.ReasoningContent) || hasJSONValue(message.ReasoningDetails) || hasJSONValue(message.Reasoning)) {
			receipt.AssistantReasoning = true
		}
		for _, call := range message.ToolCalls {
			receipt.ToolCalls = append(receipt.ToolCalls, hooksLiveToolRef{ID: call.ID, Name: call.Function.Name})
		}
		if message.Role == "tool" && message.ToolCallID != "" {
			receipt.ToolResults = append(receipt.ToolResults, message.ToolCallID)
		}
	}
	return receipt
}

func hasContextNotice(content json.RawMessage) bool {
	var text string
	return json.Unmarshal(content, &text) == nil && strings.HasPrefix(text, "NOE_CONTEXT_STATUS")
}

func hasJSONValue(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte(`""`)) && !bytes.Equal(trimmed, []byte("[]")) && !bytes.Equal(trimmed, []byte("{}"))
}

func (r *hooksLiveRecorder) add(receipt *hooksLiveWireReceipt) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	receipt.Sequence = len(r.receipts) + 1
	r.receipts = append(r.receipts, receipt)
	r.byID[receipt.Sequence] = receipt
	return receipt.Sequence
}

func (r *hooksLiveRecorder) response(id, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if receipt := r.byID[id]; receipt != nil {
		receipt.Status = status
	}
}

func (r *hooksLiveRecorder) reject(id int, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if receipt := r.byID[id]; receipt != nil {
		receipt.Rejected = reason
	}
}

func (r *hooksLiveRecorder) markResponseReasoning(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if receipt := r.byID[id]; receipt != nil {
		receipt.ResponseHasReasoning = true
	}
}

func (r *hooksLiveRecorder) snapshot() []hooksLiveWireReceipt {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]hooksLiveWireReceipt, len(r.receipts))
	for i, receipt := range r.receipts {
		result[i] = *receipt
		result[i].RoleCounts = make(map[string]int, len(receipt.RoleCounts))
		for role, count := range receipt.RoleCounts {
			result[i].RoleCounts[role] = count
		}
		result[i].ToolCalls = append([]hooksLiveToolRef(nil), receipt.ToolCalls...)
		result[i].ToolResults = append([]string(nil), receipt.ToolResults...)
	}
	return result
}

type hooksLiveReasoningTap struct {
	recorder *hooksLiveRecorder
	id       int
	mu       sync.Mutex
	carry    []byte
}

type hooksLiveReadCloser struct {
	io.Reader
	io.Closer
}

func (tap *hooksLiveReasoningTap) Write(chunk []byte) (int, error) {
	tap.mu.Lock()
	combined := append(append([]byte(nil), tap.carry...), chunk...)
	if bytes.Contains(combined, []byte(`"reasoning_content"`)) || bytes.Contains(combined, []byte(`"reasoning_details"`)) {
		tap.recorder.markResponseReasoning(tap.id)
	}
	const carryLength = len(`"reasoning_details"`) - 1
	if len(combined) > carryLength {
		tap.carry = append(tap.carry[:0], combined[len(combined)-carryLength:]...)
	} else {
		tap.carry = append(tap.carry[:0], combined...)
	}
	tap.mu.Unlock()
	return len(chunk), nil
}

type hooksLiveCase struct {
	testingT testing.TB
	name     string
	artifact string
	work     string
	config   string
	events   string
	control  string
	keyFile  string
	env      []string
	proxy    *hooksLiveProxy
	recorder *hooksLiveRecorder
	mu       sync.Mutex
	summary  map[string]any
}

func newHooksLiveCase(t *testing.T, artifactRoot, name, keyFile string) *hooksLiveCase {
	t.Helper()
	artifact, err := os.MkdirTemp(artifactRoot, name+"-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(artifact, 0700); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(artifact, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	proxy := newHooksLiveProxy(t)
	temporary := t.TempDir()
	control := filepath.Join(temporary, "control")
	configPath := filepath.Join(artifact, "config.json")
	encoded, _ := json.MarshalIndent(map[string]any{"providers": map[string]any{"opencode-go": map[string]any{"baseURL": proxy.url}}}, "", "  ")
	if err := os.WriteFile(configPath, append(encoded, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	dummyCredentials := filepath.Join(temporary, "disabled-credential-file")
	c := &hooksLiveCase{
		testingT: t, name: name, artifact: artifact, work: work, config: configPath,
		events: filepath.Join(artifact, "events.jsonl"), control: control,
		keyFile: keyFile, env: makeHooksLiveEnv(configPath, control, keyFile, dummyCredentials),
		proxy: proxy, recorder: proxy.recorder,
		summary: map[string]any{
			"case": name, "provider": "opencode-go", "model": hooksLiveModel, "effort": "auto",
			"artifactDir": artifact, "workDir": work, "startedAt": time.Now().UTC().Format(time.RFC3339Nano),
			"runtime": runtime.GOOS + "/" + runtime.GOARCH + " " + runtime.Version(),
		},
	}
	if commit, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		c.summary["sourceCommit"] = strings.TrimSpace(string(commit))
	}
	t.Cleanup(func() { c.persist() })
	return c
}

func makeHooksLiveEnv(configPath, controlDir, keyFile, dummyCredentials string) []string {
	values := map[string]string{
		"NOE_CONFIG_FILE":                     configPath,
		"NOE_AGENT_RUNTIME_DIR":               controlDir,
		"NOE_AGENT_TRACE":                     "",
		"NOE_OPENCODE_GO_API_KEY_FILE":        keyFile,
		"NOE_OPENROUTER_API_KEY_FILE":         dummyCredentials,
		"NOE_OPENAI_API_KEY_FILE":             dummyCredentials,
		"NOE_OPENAI_CHATGPT_CREDENTIALS_FILE": dummyCredentials,
	}
	filtered := make([]string, 0, len(os.Environ())+len(values))
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "NOE_") {
			continue
		}
		filtered = append(filtered, item)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		filtered = append(filtered, key+"="+values[key])
	}
	return filtered
}

func (c *hooksLiveCase) write(name, content string) string {
	c.testingT.Helper()
	path := filepath.Join(c.work, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		c.testingT.Fatal(err)
	}
	return path
}

func (c *hooksLiveCase) start(args ...string) *hooksLiveProcess {
	c.testingT.Helper()
	stdoutPath := filepath.Join(c.artifact, "stdout.txt")
	stderrPath := filepath.Join(c.artifact, "stderr.txt")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		c.testingT.Fatal(err)
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		_ = stdout.Close()
		c.testingT.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	command := exec.CommandContext(ctx, cliPath, args...)
	command.Dir, command.Env, command.Stdout, command.Stderr = c.work, c.env, stdout, stderr
	if err := command.Start(); err != nil {
		cancel()
		_ = stdout.Close()
		_ = stderr.Close()
		c.testingT.Fatal(err)
	}
	process := &hooksLiveProcess{
		caseData: c, command: command, ctx: ctx, cancel: cancel,
		stdout: stdout, stderr: stderr, stdoutPath: stdoutPath, stderrPath: stderrPath,
		finished: make(chan struct{}), started: time.Now(),
	}
	go func() {
		process.waitErr = command.Wait()
		close(process.finished)
	}()
	c.summary["command"] = append([]string{"noe"}, args...)
	c.summary["deadline"] = "240s Go context; per-scenario CLI timeout is explicit"
	c.testingT.Cleanup(process.stop)
	return process
}

func (c *hooksLiveCase) startAgent(sourceName string, observe bool) *hooksLiveProcess {
	path := filepath.Join(c.work, sourceName)
	args := []string{"run", "--timeout=220s", "--events=" + c.events}
	if observe {
		args = append(args, "--observe")
	}
	args = append(args, path)
	return c.start(args...)
}

func (c *hooksLiveCase) controlCLI(args ...string) cliResult {
	c.testingT.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, cliPath, args...)
	command.Dir, command.Env = c.work, c.env
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		return cliResult{stdout: stdout.String(), stderr: ctx.Err().Error(), exitCode: -1}
	}
	result := cliResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.exitCode = exitErr.ExitCode()
		return result
	}
	result.exitCode = -1
	result.stderr = strings.TrimSpace(result.stderr + "\n" + err.Error())
	return result
}

func (c *hooksLiveCase) persist() {
	c.proxy.server.Close()
	data, err := json.MarshalIndent(c.recorder.snapshot(), "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(c.artifact, "wire-receipts.json"), append(data, '\n'), 0600)
	}
	c.mu.Lock()
	c.summary["finishedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	c.summary["wireReceipts"] = c.recorder.snapshot()
	c.mu.Unlock()
	encoded, summaryErr := json.MarshalIndent(c.summary, "", "  ")
	if summaryErr == nil {
		summaryErr = os.WriteFile(filepath.Join(c.artifact, "summary.json"), append(encoded, '\n'), 0600)
	}
	if err != nil || summaryErr != nil {
		c.testingT.Errorf("save live artifacts: %v", errors.Join(err, summaryErr))
	}
}

type hooksLiveProcess struct {
	caseData   *hooksLiveCase
	command    *exec.Cmd
	ctx        context.Context
	cancel     context.CancelFunc
	stdout     *os.File
	stderr     *os.File
	stdoutPath string
	stderrPath string
	finished   chan struct{}
	waitErr    error
	started    time.Time
	once       sync.Once
}

func (process *hooksLiveProcess) stop() {
	process.cancel()
	select {
	case <-process.finished:
	case <-time.After(8 * time.Second):
		if process.command.Process != nil {
			_ = process.command.Process.Kill()
		}
		<-process.finished
	}
	process.closeFiles()
}

func (process *hooksLiveProcess) closeFiles() {
	process.once.Do(func() {
		_ = process.stdout.Close()
		_ = process.stderr.Close()
	})
}

func (process *hooksLiveProcess) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-process.finished:
	case <-process.ctx.Done():
		<-process.finished
	}
	process.cancel()
	process.closeFiles()
	exitCode := -1
	if process.command.ProcessState != nil {
		exitCode = process.command.ProcessState.ExitCode()
	}
	err := process.waitErr
	if process.ctx.Err() != nil && errors.Is(process.ctx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("live CLI exceeded its bounded timeout: %w", process.ctx.Err())
	}
	process.caseData.summary["exitCode"] = exitCode
	process.caseData.summary["durationMs"] = time.Since(process.started).Milliseconds()
	if err != nil {
		process.caseData.summary["processError"] = err.Error()
	}
	return err
}

func (process *hooksLiveProcess) finishedNow() bool {
	select {
	case <-process.finished:
		return true
	default:
		return false
	}
}

func (c *hooksLiveCase) eventRows() ([]map[string]json.RawMessage, error) {
	data, err := os.ReadFile(c.events)
	if err != nil {
		return nil, err
	}
	var rows []map[string]json.RawMessage
	for lineNo, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row map[string]json.RawMessage
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("event line %d: %w", lineNo+1, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (c *hooksLiveCase) readOutput(name string) string {
	c.testingT.Helper()
	data, err := os.ReadFile(filepath.Join(c.artifact, name))
	if err != nil {
		c.testingT.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func eventString(row map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(row[key], &value)
	return value
}

func eventNumber(row map[string]json.RawMessage, key string) int64 {
	var value int64
	_ = json.Unmarshal(row[key], &value)
	return value
}

func writeHooksLiveJSONL(t *testing.T, path string, values []map[string]any) {
	t.Helper()
	var buffer bytes.Buffer
	writer := bufio.NewWriter(&buffer)
	for _, value := range values {
		if err := json.NewEncoder(writer).Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func waitHooksLiveFile(ctx context.Context, process *hooksLiveProcess, path string) ([]byte, error) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if data, err := os.ReadFile(path); err == nil {
			return data, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if process.finishedNow() {
			return nil, fmt.Errorf("CLI ended before readiness file %s appeared", filepath.Base(path))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitHooksLivePredicate(ctx context.Context, process *hooksLiveProcess, predicate func() (bool, error)) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := predicate()
		if err != nil || ok {
			return err
		}
		if process.finishedNow() {
			return errors.New("CLI ended before condition was met")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func runHooksLiveL1(t *testing.T, artifactRoot, keyFile string) {
	c := newHooksLiveCase(t, artifactRoot, "l1-notice-tool", keyFile)
	writeFixture(t, filepath.Join(c.work, "numbers.json"), `{"values":[10,20,30,40]}`)
	receiptPath, _ := json.Marshal(filepath.Join(c.work, "hook-receipts.jsonl"))
	numbersPath, _ := json.Marshal(filepath.Join(c.work, "numbers.json"))
	resultPath, _ := json.Marshal(filepath.Join(c.work, "result.json"))
	snapshotPath, _ := json.Marshal(filepath.Join(c.work, "result-summary.json"))
	c.write("hooks.js", `import {appendFile} from "node:fs/promises";
const receiptPath=`+string(receiptPath)+`;
async function receipt(phase,event,extra={}){
  const row={phase,activityId:event.activityId,invocationId:event.invocationId,groupId:event.groupId,
    agentId:event.agentId,runId:event.runId,modelStepId:event.modelStepId,requestId:event.requestId,
    toolCallId:event.toolCallId,toolName:event.toolName,agentDepth:event.agentDepth,...extra};
  await appendFile(receiptPath,JSON.stringify(row)+"\n");
}
export async function beforeModelRequest(event,api){
  const context=await api.context.get();
  await api.context.setRuntimeInfo({key:"live-test",text:"Use run_js for the requested calculation; keep the final answer brief."});
  await receipt("beforeModelRequest",event,{budget:context.budget?.inputTokens??null});
}
export async function afterModelResponse(event){
  await receipt("afterModelResponse",event,{responseRequestId:event.response?.requestId??null});
}
export async function beforeTool(event){
  await receipt("beforeTool",event);
  if(event.toolName!=="run_js") return;
  const code="// Read the authorized fixture and write its computed result.\n"+
    "import {readFile,writeFile} from 'node:fs/promises';\n"+
    "const values=JSON.parse(await readFile("+JSON.stringify(`+string(numbersPath)+`)+",'utf8')).values;\n"+
    "const total=values.reduce((sum,value)=>sum+value,0);\n"+
    "const result={count:values.length,total,average:total/values.length};\n"+
    "await writeFile("+JSON.stringify(`+string(resultPath)+`)+",JSON.stringify(result));\n"+
    "console.log(JSON.stringify(result));";
  return {arguments:{code}};
}
export async function afterTool(event){
  await receipt("afterTool",event);
  return {result:{hookMarker:"afterTool",stdout:event.result?.stdout??""}};
}
`)
	main := `import {run_agent} from "noe:agent";
import {writeFile} from "node:fs/promises";
const result=await run_agent({provider:"opencode-go",model:"deepseek-v4.1-flash",effort:"auto",name:"hooks-live-l1",
  hooks:["./hooks.js"],contextBudget:{inputTokens:16000},executionLimits:{providerCalls:8,agentStarts:1,queuedTasks:0,hookCalls:40},
  maxTokens:4096,maxSteps:4},
  "Read numbers.json with run_js, calculate count, total, and average, write result.json, then give a short answer containing those values. Do not call decision APIs or any provider other than this agent.");
await writeFile(` + string(snapshotPath) + `,JSON.stringify({state:result.lastResult?.state,answer:result.lastResult?.answer,execution:result.execution}));`
	c.write("main.js", main)
	process := c.startAgent("main.js", true)
	if err := process.wait(t); err != nil {
		t.Fatalf("live CLI failed; inspect artifacts at %s", c.artifact)
	}
	if process.command.ProcessState.ExitCode() != 0 {
		t.Fatalf("live CLI exit=%d; inspect artifacts at %s", process.command.ProcessState.ExitCode(), c.artifact)
	}
	stdout := c.readOutput("stdout.txt")
	outputBytes, err := os.ReadFile(filepath.Join(c.work, "result-summary.json"))
	if err != nil {
		t.Fatalf("live script did not write result summary; inspect artifacts at %s", c.artifact)
	}
	var output struct {
		State  string `json:"state"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(outputBytes, &output); err != nil {
		t.Fatalf("live agent result is not JSON; inspect artifacts at %s", c.artifact)
	}
	if output.State != "completed" || !strings.Contains(output.Answer, "100") {
		t.Errorf("real task did not return the computed result (state=%s); inspect artifacts at %s", output.State, c.artifact)
	}
	resultBytes, err := os.ReadFile(filepath.Join(c.work, "result.json"))
	if err != nil {
		t.Fatalf("run_js did not write result.json; inspect artifacts at %s", c.artifact)
	}
	var calculation struct {
		Count   int `json:"count"`
		Total   int `json:"total"`
		Average int `json:"average"`
	}
	if json.Unmarshal(resultBytes, &calculation) != nil || calculation.Count != 4 || calculation.Total != 100 || calculation.Average != 25 {
		t.Errorf("wrong fixture calculation %s", resultBytes)
	}
	if !strings.Contains(stdout, "\ncontext:") || strings.Contains(stdout, "context: budget=") {
		t.Errorf("--observe did not render context percentage alongside the answer; inspect artifacts at %s", c.artifact)
	}
	rows, err := c.eventRows()
	if err != nil {
		t.Fatalf("read JSONL events: %v", err)
	}
	hookRows, err := readHooksLiveJSONL(filepath.Join(c.work, "hook-receipts.jsonl"))
	if err != nil {
		t.Fatalf("read hook receipts: %v", err)
	}
	assertHooksLiveL1(t, c, rows, hookRows)
	t.Logf("live L1 artifacts: %s", c.artifact)
}

func readHooksLiveJSONL(path string) ([]map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []map[string]json.RawMessage
	for lineNo, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row map[string]json.RawMessage
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("hook line %d: %w", lineNo+1, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func assertHooksLiveL1(t *testing.T, c *hooksLiveCase, events, hooks []map[string]json.RawMessage) {
	t.Helper()
	modelStarted, modelFinished := 0, 0
	agents, requests := make(map[string]bool), make(map[string]bool)
	usageFound := false
	toolEvents := make(map[string]map[string]json.RawMessage)
	for _, event := range events {
		switch eventString(event, "type") {
		case "model.started":
			modelStarted++
			agents[eventString(event, "agentId")] = true
			requests[eventString(event, "requestId")] = true
		case "model.finished":
			modelFinished++
			var usage map[string]json.RawMessage
			_ = json.Unmarshal(event["usage"], &usage)
			for _, key := range []string{"promptTokens", "completionTokens", "totalTokens"} {
				if eventNumber(usage, key) > 0 {
					usageFound = true
				}
			}
		case "tool.finished":
			toolEvents[eventString(event, "toolCallId")] = event
		}
	}
	if modelStarted == 0 || modelFinished == 0 || !usageFound {
		t.Errorf("real provider request/usage missing: started=%d finished=%d measuredUsage=%v", modelStarted, modelFinished, usageFound)
	}
	phaseCounts := make(map[string]int)
	beforeTools := make(map[string]map[string]json.RawMessage)
	afterTools := make(map[string]map[string]json.RawMessage)
	for _, hook := range hooks {
		phase := eventString(hook, "phase")
		phaseCounts[phase]++
		if phase == "beforeTool" {
			beforeTools[eventString(hook, "toolCallId")] = hook
		}
		if phase == "afterTool" {
			afterTools[eventString(hook, "toolCallId")] = hook
		}
		if phase == "afterModelResponse" {
			id := eventString(hook, "responseRequestId")
			if id == "" || !requests[id] {
				t.Errorf("afterModelResponse request ID did not match model.started: %q", id)
			}
		}
	}
	for _, phase := range []string{"beforeModelRequest", "afterModelResponse", "beforeTool", "afterTool"} {
		if phaseCounts[phase] == 0 {
			t.Errorf("hook phase %s was not exercised", phase)
		}
	}
	if len(beforeTools) == 0 || len(beforeTools) != len(afterTools) {
		t.Errorf("tool hooks did not pair: before=%d after=%d", len(beforeTools), len(afterTools))
	}
	for toolID, before := range beforeTools {
		after := afterTools[toolID]
		if after == nil || eventString(before, "modelStepId") != eventString(after, "modelStepId") || toolEvents[toolID] == nil {
			t.Errorf("tool hook/model result correlation missing for %s", toolID)
		}
		var effective, modelResult map[string]json.RawMessage
		_ = json.Unmarshal(toolEvents[toolID]["arguments"], &effective)
		_ = json.Unmarshal(toolEvents[toolID]["result"], &modelResult)
		if eventString(effective, "code") == "" || eventString(modelResult, "hookMarker") != "afterTool" {
			t.Errorf("effective tool arguments or afterTool result marker missing for %s", toolID)
		}
	}
	wires := c.recorder.snapshot()
	if len(wires) == 0 {
		t.Fatal("provider proxy captured no actual OpenCode requests")
	}
	for _, wire := range wires {
		if wire.Rejected != "" || wire.Status < 200 || wire.Status >= 300 || wire.Model != hooksLiveModel {
			t.Errorf("unexpected or rejected provider wire request sequence %d: status=%d rejected=%s model=%s", wire.Sequence, wire.Status, wire.Rejected, wire.Model)
		}
		if !strings.HasPrefix(wire.UserAgent, "noe/") || wire.SessionID == "" || !agents[wire.SessionID] {
			t.Errorf("OpenCode User-Agent/session correlation missing on request %d", wire.Sequence)
		}
		if wire.ContextNoticeCount < 1 || !wire.ContextNoticeAtTail {
			t.Errorf("wire request %d has context notice count=%d tail=%v", wire.Sequence, wire.ContextNoticeCount, wire.ContextNoticeAtTail)
		}
	}
	for i, wire := range wires {
		for _, call := range wire.ToolCalls {
			if !containsString(wire.ToolResults, call.ID) {
				t.Errorf("tool call %s was not replayed with a tool result in request %d", call.ID, wire.Sequence)
			}
		}
		if !wire.ResponseHasReasoning {
			continue
		}
		var next *hooksLiveWireReceipt
		for j := i + 1; j < len(wires); j++ {
			if wires[j].SessionID == wire.SessionID {
				next = &wires[j]
				break
			}
		}
		if next != nil && !next.AssistantReasoning {
			t.Errorf("provider reasoning was present before tool replay but absent from request %d", next.Sequence)
		}
	}
	c.mu.Lock()
	c.summary["jsonlModelStarted"] = modelStarted
	c.summary["jsonlModelFinished"] = modelFinished
	c.summary["usageObserved"] = usageFound
	c.summary["hookPhaseCounts"] = phaseCounts
	c.mu.Unlock()
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func runHooksLiveL2(t *testing.T, artifactRoot, keyFile string) {
	c := newHooksLiveCase(t, artifactRoot, "l2-descendants-queue", keyFile)
	receiptsPath, _ := json.Marshal(filepath.Join(c.work, "hook-receipts.jsonl"))
	rootMarker, _ := json.Marshal(filepath.Join(c.work, "root-spawned.once"))
	childMarker, _ := json.Marshal(filepath.Join(c.work, "child-spawned.once"))
	c.write("hooks.js", `import {appendFile} from "node:fs/promises";
import {existsSync as fileExists,writeFileSync} from "node:fs";
const receiptPath=`+string(receiptsPath)+`;
async function receipt(phase,event,extra={}){
  await appendFile(receiptPath,JSON.stringify({phase,activityId:event.activityId,invocationId:event.invocationId,
    groupId:event.groupId,agentId:event.agentId,runId:event.runId,parentRunId:event.parentRunId,
    modelStepId:event.modelStepId,requestId:event.requestId,agentDepth:event.agentDepth,
    causedBy:event.causedBy,origin:event.origin,...extra})+"\n");
}
export async function beforeModelRequest(event,api){
  await receipt("beforeModelRequest",event);
  if(event.agentDepth===0&&!fileExists(`+string(rootMarker)+`)){
    writeFileSync(`+string(rootMarker)+`,"started");
    const child=await api.agents.run_agent({},"Finish the child task briefly. Do not use tools or decision APIs.",{allowDelegation:true});
    await receipt("rootChildCompleted",event,{childState:child.lastResult?.state});
  }
  if(event.agentDepth===1&&!fileExists(`+string(childMarker)+`)){
    writeFileSync(`+string(childMarker)+`,"started");
    const {run_agent}=await import("noe:agent");
    const grandchild=await run_agent({},"Finish the grandchild task briefly. Do not use tools or decision APIs.");
    await receipt("childGrandchildCompleted",event,{grandchildState:grandchild.lastResult?.state});
  }
}
export async function afterModelResponse(event){await receipt("afterModelResponse",event,{responseRequestId:event.response?.requestId??null});}
export async function beforeTool(event){
  await receipt("beforeTool",event);
  if(event.toolName==="run_js") return {arguments:{code:"// Return a safe notice for an unnecessary tool call.\nconsole.log('No tool was needed.');"}};
}
export async function afterTool(event){await receipt("afterTool",event);return {result:event.result};}
`)
	main := `import {spawn_agent,observe_agent,queue_agent,interrupt_agent} from "noe:agent";
import {readFile} from "node:fs/promises";
const receiptPath=` + string(receiptsPath) + `;
const config={provider:"opencode-go",model:"deepseek-v4.1-flash",effort:"auto",name:"root-live",
  hooks:["./hooks.js"],executionLimits:{providerCalls:14,agentStarts:3,queuedTasks:1,hookCalls:100},maxTokens:4096,maxSteps:4};
const {agentId}=await spawn_agent(config,"Complete this root task briefly after its child tasks. Do not use tools or decision APIs.",{keepAlive:true});
const pause=()=>new Promise(resolve=>setTimeout(resolve,20));
async function waitSnapshot(predicate,label){for(let i=0;i<10000;i++){const value=await observe_agent({agentId});if(predicate(value))return value;await pause();}throw new Error("timed out waiting for "+label);}
const first=await waitSnapshot(s=>s.state==="idle"&&s.lastResult?.state==="completed","first root task");
const firstRunId=first.lastResult.runId;
const queue1=await queue_agent({agentId,instruction:"Complete the queued task briefly without tools or decision APIs.",commandId:"hooks-live-queue-l2"});
const queueReplay=await queue_agent({agentId,instruction:"Complete the queued task briefly without tools or decision APIs.",commandId:"hooks-live-queue-l2"});
for(let i=0;i<10000;i++){
  const text=await readFile(receiptPath,"utf8").catch(()=>"");
  const count=text.split("\n").filter(line=>{try{const e=JSON.parse(line);return e.phase==="afterModelResponse"&&e.agentId===agentId;}catch{return false;}}).length;
  if(count>=2)break;
  if(i===9999)throw new Error("queued root task did not reach a model response");
  await pause();
}
const second=await waitSnapshot(s=>s.state==="idle"&&s.queueLength===0&&s.lastResult?.state==="completed"&&s.lastResult.runId!==firstRunId,"queued root task");
const accepted=await interrupt_agent({agentId});
const terminal=await waitSnapshot(s=>s.state==="stopped","root terminal");
console.log(JSON.stringify({agentId,first,queue1,queueReplay,second,accepted,terminal}));`
	c.write("main.js", main)
	process := c.startAgent("main.js", false)
	if err := process.wait(t); err != nil || process.command.ProcessState.ExitCode() != 0 {
		t.Fatalf("live L2 CLI failed; inspect artifacts at %s", c.artifact)
	}
	output := hooksLiveLastJSONLine(t, c.readOutput("stdout.txt"), c.artifact)
	var report struct {
		AgentID string `json:"agentId"`
		First   struct {
			LastResult struct {
				RunID string `json:"runId"`
			} `json:"lastResult"`
		} `json:"first"`
		Second struct {
			LastResult struct {
				RunID string `json:"runId"`
			} `json:"lastResult"`
		} `json:"second"`
		Terminal struct {
			State       string `json:"state"`
			Reason      string `json:"reason"`
			GroupID     string `json:"groupId"`
			QueueLength int    `json:"queueLength"`
			Execution   struct {
				Counters struct {
					ProviderCalls int64 `json:"providerCalls"`
					AgentStarts   int64 `json:"agentStarts"`
					QueuedTasks   int64 `json:"queuedTasks"`
					ActiveAgents  int64 `json:"activeAgents"`
					ActiveTasks   int64 `json:"activeTasks"`
				} `json:"counters"`
			} `json:"execution"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("L2 summary is not JSON; inspect artifacts at %s", c.artifact)
	}
	rows, err := c.eventRows()
	if err != nil {
		t.Fatalf("read L2 events: %v", err)
	}
	hooks, err := readHooksLiveJSONL(filepath.Join(c.work, "hook-receipts.jsonl"))
	if err != nil {
		t.Fatalf("read L2 hook receipts: %v", err)
	}
	if report.AgentID == "" || report.Terminal.State != "stopped" || report.Terminal.Reason != "interrupted" || report.Terminal.GroupID != report.AgentID {
		t.Errorf("root did not stop in its own group: state=%s reason=%s group=%s agent=%s", report.Terminal.State, report.Terminal.Reason, report.Terminal.GroupID, report.AgentID)
	}
	if report.First.LastResult.RunID == "" || report.Second.LastResult.RunID == "" || report.First.LastResult.RunID == report.Second.LastResult.RunID {
		t.Errorf("queued task did not receive a fresh run ID: first=%s second=%s", report.First.LastResult.RunID, report.Second.LastResult.RunID)
	}
	counters := report.Terminal.Execution.Counters
	if counters.AgentStarts != 3 || counters.QueuedTasks != 1 || counters.ActiveAgents != 0 || counters.ActiveTasks != 0 || report.Terminal.QueueLength != 0 {
		t.Errorf("shared execution counters/cleanup mismatch: %+v queue=%d", counters, report.Terminal.QueueLength)
	}
	assertHooksLiveGroupWire(t, c, rows, report.AgentID, 3) // Two root queue tasks share one session.
	assertHooksLiveAgentTree(t, rows, hooks, report.AgentID, report.First.LastResult.RunID, report.Second.LastResult.RunID)
	if entries, err := os.ReadDir(c.control); err == nil && len(entries) != 0 {
		t.Errorf("control registration remained after root CLI exit: %d files", len(entries))
	}
	t.Logf("live L2 artifacts: %s", c.artifact)
}

func hooksLiveLastJSONLine(t *testing.T, output, artifact string) string {
	t.Helper()
	trimmed := strings.TrimSpace(output)
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 || !json.Valid([]byte(strings.TrimSpace(lines[len(lines)-1]))) {
		t.Fatalf("last CLI output line is not JSON; inspect artifacts at %s", artifact)
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

func assertHooksLiveGroupWire(t *testing.T, c *hooksLiveCase, events []map[string]json.RawMessage, groupID string, minimumSessions int) {
	t.Helper()
	agentIDs := make(map[string]bool)
	for _, row := range events {
		if eventString(row, "type") == "model.started" && eventString(row, "groupId") == groupID {
			agentIDs[eventString(row, "agentId")] = true
		}
	}
	wires := c.recorder.snapshot()
	sessions := make(map[string]bool)
	for _, wire := range wires {
		if wire.Rejected != "" || wire.Status < 200 || wire.Status >= 300 || wire.Model != hooksLiveModel {
			t.Errorf("invalid authorized live request sequence %d", wire.Sequence)
		}
		if !strings.HasPrefix(wire.UserAgent, "noe/") || !agentIDs[wire.SessionID] {
			t.Errorf("wire session %q did not match an agent instance or User-Agent was missing", wire.SessionID)
		}
		if wire.ContextNoticeCount < 1 || !wire.ContextNoticeAtTail {
			t.Errorf("request %d did not have a context notice at the tail", wire.Sequence)
		}
		sessions[wire.SessionID] = true
	}
	if len(sessions) < minimumSessions {
		t.Errorf("only %d distinct real instance sessions; want at least %d", len(sessions), minimumSessions)
	}
}

func assertHooksLiveAgentTree(t *testing.T, events, hooks []map[string]json.RawMessage, rootID, firstRootRun, queuedRootRun string) {
	t.Helper()
	instances := make([]map[string]json.RawMessage, 0)
	runParents := make(map[string]string)
	runAgents := make(map[string]string)
	groupAgents := make(map[string]bool)
	for _, row := range events {
		switch eventString(row, "type") {
		case "instance.started":
			instances = append(instances, row)
			if eventString(row, "groupId") == rootID {
				groupAgents[eventString(row, "agentId")] = true
			}
		case "agent.started":
			runID := eventString(row, "runId")
			runParents[runID] = eventString(row, "parentRunId")
			runAgents[runID] = eventString(row, "agentId")
			if eventString(row, "groupId") != rootID {
				t.Errorf("task %s escaped group %s", runID, rootID)
			}
		}
	}
	childFound, grandchildFound := false, false
	for agentID := range groupAgents {
		if agentID == rootID {
			continue
		}
		for runID, parentRun := range runParents {
			if runAgents[runID] != agentID {
				continue
			}
			if parentRun == firstRootRun || parentRun == queuedRootRun {
				childFound = true
			}
			for childRun, childAgent := range runAgents {
				if childAgent == agentID || childAgent == rootID || childAgent == "" {
					continue
				}
				if runParents[childRun] == runID {
					grandchildFound = true
				}
			}
		}
	}
	if !childFound || !grandchildFound {
		t.Errorf("did not observe owned child and grandchild: instances=%d parents=%v agents=%v", len(instances), runParents, runAgents)
	}
	depths := make(map[int]bool)
	for _, hook := range hooks {
		var depth int
		_ = json.Unmarshal(hook["agentDepth"], &depth)
		depths[depth] = true
		if eventString(hook, "groupId") != rootID {
			t.Errorf("inherited hook escaped root group: %+v", hook)
		}
	}
	for _, depth := range []int{0, 1, 2} {
		if !depths[depth] {
			t.Errorf("hook modules did not run at agentDepth %d", depth)
		}
	}
}

func runHooksLiveSteer(t *testing.T, artifactRoot, keyFile string) {
	c := newHooksLiveCase(t, artifactRoot, "l3-steer-in-hook", keyFile)
	readyPath := filepath.Join(c.work, "hook-ready.json")
	releasePath := filepath.Join(c.work, "release-hook")
	readyJSON, _ := json.Marshal(readyPath)
	releaseJSON, _ := json.Marshal(releasePath)
	receiptJSON, _ := json.Marshal(filepath.Join(c.work, "hook-receipts.jsonl"))
	c.write("hooks.js", `import {appendFile} from "node:fs/promises";
import {existsSync,writeFileSync} from "node:fs";
const receiptPath=`+string(receiptJSON)+`;
async function receipt(phase,event){await appendFile(receiptPath,JSON.stringify({phase,activityId:event.activityId,groupId:event.groupId,agentId:event.agentId,runId:event.runId,modelStepId:event.modelStepId,requestId:event.requestId})+"\n");}
export async function beforeModelRequest(event){
  await receipt("beforeModelRequest",event);
  writeFileSync(`+string(readyJSON)+`,JSON.stringify({runId:event.runId,modelStepId:event.modelStepId,groupId:event.groupId}));
  while(!existsSync(`+string(releaseJSON)+`)) await new Promise(resolve=>setTimeout(resolve,10));
}
export async function afterModelResponse(event){await receipt("afterModelResponse",event);}
export async function beforeTool(event){
  if(event.toolName==="run_js") return {arguments:{code:"// Make an unnecessary tool call safe.\nconsole.log('No tool was needed.');"}};
}
`)
	c.write("main.js", `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"opencode-go",model:"deepseek-v4.1-flash",effort:"auto",name:"steer-live",
  hooks:["./hooks.js"],executionLimits:{providerCalls:4,agentStarts:1,queuedTasks:0,hookCalls:12},maxTokens:4096,maxSteps:4},
  "Wait for any instruction delivered while the beforeModelRequest hook is active. Include marker NOE_LIVE_STEER_MARKER_20261018 in your brief answer if supplied. Do not use tools or decision APIs.");
console.log(JSON.stringify({lastResult:result.lastResult,execution:result.execution}));`)
	process := c.startAgent("main.js", false)
	readyCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	readyBytes, err := waitHooksLiveFile(readyCtx, process, readyPath)
	if err != nil {
		t.Fatalf("beforeModelRequest did not reach its readiness gate: %v; artifacts at %s", err, c.artifact)
	}
	var ready struct {
		RunID       string `json:"runId"`
		ModelStepID string `json:"modelStepId"`
	}
	if json.Unmarshal(readyBytes, &ready) != nil || ready.RunID == "" || ready.ModelStepID == "" {
		t.Fatalf("invalid hook readiness metadata; artifacts at %s", c.artifact)
	}
	const marker = "NOE_LIVE_STEER_MARKER_20261018"
	control := c.controlCLI("steer", ready.RunID, "Include "+marker+" in your brief answer.")
	if control.exitCode != 0 || !strings.Contains(control.stdout, "steer accepted:") {
		t.Fatalf("steer was not accepted during the hook; artifacts at %s", c.artifact)
	}
	writeFixture(t, releasePath, "release")
	if err := process.wait(t); err != nil || process.command.ProcessState.ExitCode() != 0 {
		t.Fatalf("steer scenario did not settle; artifacts at %s", c.artifact)
	}
	rows, err := c.eventRows()
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := readHooksLiveJSONL(filepath.Join(c.work, "hook-receipts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	beforeCount, applied := 0, false
	for _, hook := range hooks {
		if eventString(hook, "phase") == "beforeModelRequest" && eventString(hook, "runId") == ready.RunID && eventString(hook, "modelStepId") == ready.ModelStepID {
			beforeCount++
		}
	}
	for _, event := range rows {
		if eventString(event, "runId") == ready.RunID && eventString(event, "type") == "command.applied" && eventString(event, "operation") == "steer" {
			applied = true
		}
	}
	if beforeCount != 1 || !applied {
		t.Errorf("steer did not apply once within the original prepared step: before=%d applied=%v", beforeCount, applied)
	}
	wires := c.recorder.snapshot()
	if len(wires) == 0 || !wires[0].SteerMarkerPresent {
		t.Errorf("provider wire request did not contain the accepted steer marker")
	}
	assertHooksLiveGroupWire(t, c, rows, eventStringForRun(rows, ready.RunID, "groupId"), 1)
	c.mu.Lock()
	c.summary["steerRunId"] = ready.RunID
	c.summary["steerModelStepId"] = ready.ModelStepID
	c.summary["steerApplied"] = applied
	c.summary["beforeHookCount"] = beforeCount
	c.mu.Unlock()
	t.Logf("live L3 steer artifacts: %s", c.artifact)
}

func eventStringForRun(events []map[string]json.RawMessage, runID, key string) string {
	for _, event := range events {
		if eventString(event, "runId") == runID && eventString(event, "groupId") != "" {
			return eventString(event, key)
		}
	}
	return ""
}

func runHooksLiveHookCancel(t *testing.T, artifactRoot, keyFile string) {
	c := newHooksLiveCase(t, artifactRoot, "l3-cancel-stalled-hook", keyFile)
	readyPath := filepath.Join(c.work, "hook-ready.json")
	readyJSON, _ := json.Marshal(readyPath)
	receiptJSON, _ := json.Marshal(filepath.Join(c.work, "hook-receipts.jsonl"))
	c.write("hooks.js", `import {appendFile,writeFile} from "node:fs/promises";
async function receipt(phase,event,extra={}){await appendFile(`+string(receiptJSON)+`,JSON.stringify({phase,activityId:event.activityId,groupId:event.groupId,agentId:event.agentId,runId:event.runId,modelStepId:event.modelStepId,requestId:event.requestId,...extra})+"\n");}
export async function afterModelResponse(event,api){
  await receipt("afterModelResponse",event);
  const child=await api.agents.spawn_agent({},undefined,{keepAlive:true});
  await writeFile(`+string(readyJSON)+`,JSON.stringify({runId:event.runId,modelStepId:event.modelStepId,childAgentId:child.agentId}));
  setInterval(()=>{},1000);
  await new Promise(()=>{});
}
export async function beforeTool(event){
  if(event.toolName==="run_js") return {arguments:{code:"// Make an unnecessary tool call safe.\nconsole.log('No tool was needed.');"}};
}
`)
	c.write("main.js", `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"opencode-go",model:"deepseek-v4.1-flash",effort:"auto",name:"cancel-hook-live",
  hooks:["./hooks.js"],executionLimits:{providerCalls:3,agentStarts:2,queuedTasks:0,hookCalls:12},maxTokens:4096,maxSteps:3},
  "Give one short answer. Do not use tools or decision APIs.");
console.log(JSON.stringify({lastResult:result.lastResult,execution:result.execution}));`)
	process := c.startAgent("main.js", false)
	readyCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	readyBytes, err := waitHooksLiveFile(readyCtx, process, readyPath)
	if err != nil {
		t.Fatalf("afterModelResponse did not reach its stall gate: %v; artifacts at %s", err, c.artifact)
	}
	var ready struct {
		RunID        string `json:"runId"`
		ChildAgentID string `json:"childAgentId"`
	}
	if json.Unmarshal(readyBytes, &ready) != nil || ready.RunID == "" || ready.ChildAgentID == "" {
		t.Fatalf("invalid stalled-hook readiness metadata; artifacts at %s", c.artifact)
	}
	control := c.controlCLI("interrupt", "--run", ready.RunID)
	if control.exitCode != 0 || !strings.Contains(control.stdout, "interrupt accepted:") {
		t.Fatalf("run interrupt was not accepted; artifacts at %s", c.artifact)
	}
	if err := process.wait(t); err != nil || process.command.ProcessState.ExitCode() != 0 {
		t.Fatalf("stalled hook did not cancel cleanly; artifacts at %s", c.artifact)
	}
	rows, err := c.eventRows()
	if err != nil {
		t.Fatal(err)
	}
	hookCancelled, childStopped, activeZero := false, false, false
	rootAgentID := eventStringForRun(rows, ready.RunID, "agentId")
	for _, event := range rows {
		switch eventString(event, "type") {
		case "hook.cancelled":
			if eventString(event, "runId") == ready.RunID {
				hookCancelled = true
			}
		case "instance.stopped", "instance.failed":
			if eventString(event, "agentId") == ready.ChildAgentID {
				childStopped = true
			}
			if rootAgentID != "" && eventString(event, "agentId") == rootAgentID {
				var execution struct {
					Counters *struct {
						ActiveAgents int64 `json:"activeAgents"`
						ActiveTasks  int64 `json:"activeTasks"`
					} `json:"counters"`
				}
				if err := json.Unmarshal(event["execution"], &execution); err != nil {
					t.Errorf("invalid root terminal execution snapshot: %v", err)
					continue
				}
				if execution.Counters == nil {
					t.Error("root terminal execution snapshot has no counters")
					continue
				}
				activeZero = execution.Counters.ActiveAgents == 0 && execution.Counters.ActiveTasks == 0
			}
		}
	}
	if !hookCancelled || !childStopped || !activeZero {
		t.Errorf("hook cancellation cleanup incomplete: hookCancelled=%v childStopped=%v activeZero=%v", hookCancelled, childStopped, activeZero)
	}
	assertHooksLiveGroupWire(t, c, rows, eventStringForRun(rows, ready.RunID, "groupId"), 1)
	if entries, err := os.ReadDir(c.control); err == nil && len(entries) != 0 {
		t.Errorf("control registration remained after cancellation: %d files", len(entries))
	}
	c.mu.Lock()
	c.summary["cancelledRunId"] = ready.RunID
	c.summary["childAgentId"] = ready.ChildAgentID
	c.summary["hookCancelled"] = hookCancelled
	c.summary["childStopped"] = childStopped
	c.mu.Unlock()
	t.Logf("live L3 stalled-hook artifacts: %s", c.artifact)
}

func runHooksLiveToolCancel(t *testing.T, artifactRoot, keyFile string) {
	c := newHooksLiveCase(t, artifactRoot, "l3-cancel-run-js-tree", keyFile)
	readyPath := filepath.Join(c.work, "tool-ready.json")
	rootPIDPath := filepath.Join(c.work, "run-js.pid")
	shellPIDPath := filepath.Join(c.work, "shell.pid")
	grandchildPIDPath := filepath.Join(c.work, "grandchild.pid")
	readyJSON, _ := json.Marshal(readyPath)
	rootPIDJSON, _ := json.Marshal(rootPIDPath)
	shellPIDJSON, _ := json.Marshal(shellPIDPath)
	grandchildPIDJSON, _ := json.Marshal(grandchildPIDPath)
	receiptJSON, _ := json.Marshal(filepath.Join(c.work, "hook-receipts.jsonl"))
	c.write("hooks.js", `import {appendFile,writeFile} from "node:fs/promises";
async function receipt(phase,event){await appendFile(`+string(receiptJSON)+`,JSON.stringify({phase,activityId:event.activityId,groupId:event.groupId,agentId:event.agentId,runId:event.runId,modelStepId:event.modelStepId,requestId:event.requestId,toolCallId:event.toolCallId})+"\n");}
export async function beforeTool(event){
  await receipt("beforeTool",event);
  if(event.toolName!=="run_js") return;
  await writeFile(`+string(readyJSON)+`,JSON.stringify({runId:event.runId,modelStepId:event.modelStepId,toolCallId:event.toolCallId}));
  const code="// Start an owned shell process tree and wait for cancellation.\n"+
    "import {writeFileSync} from 'node:fs';\n"+
    "import {spawn} from 'noe:process';\n"+
    "writeFileSync("+JSON.stringify(`+string(rootPIDJSON)+`)+",String(process.pid));\n"+
    "const child=await spawn('sh',['-c','echo $$ > \"$1\"; sleep 120 & echo $! > \"$2\"; wait','hooks-live-tree',"+
      JSON.stringify(`+string(shellPIDJSON)+`)+","+JSON.stringify(`+string(grandchildPIDJSON)+`)+"],{stdin:'ignore',stdout:'ignore',stderr:'ignore'});\n"+
    "await child.exited;";
  return {arguments:{code}};
}
export async function afterTool(event){await receipt("afterTool",event);return {result:event.result};}
`)
	c.write("main.js", `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"opencode-go",model:"deepseek-v4.1-flash",effort:"auto",name:"cancel-tool-live",
  hooks:["./hooks.js"],executionLimits:{providerCalls:4,agentStarts:1,queuedTasks:0,hookCalls:12},maxTokens:4096,maxSteps:3},
  "Call run_js now with exactly this JavaScript: // Start the test process tree.\nconsole.log('ready');\nThe beforeTool hook replaces this code with a deterministic owned shell process tree and waits for external cancellation. Do not inspect files, discover commands, or spawn agents. Do not call decision APIs or any provider other than this agent.");
console.log(JSON.stringify({lastResult:result.lastResult,execution:result.execution}));`)
	process := c.startAgent("main.js", false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	readyBytes, err := waitHooksLiveFile(ctx, process, readyPath)
	if err != nil {
		t.Fatalf("run_js hook did not reach readiness: %v; artifacts at %s", err, c.artifact)
	}
	var ready struct {
		RunID string `json:"runId"`
	}
	if json.Unmarshal(readyBytes, &ready) != nil || ready.RunID == "" {
		t.Fatalf("invalid tool readiness metadata; artifacts at %s", c.artifact)
	}
	var pids []int
	for _, path := range []string{rootPIDPath, shellPIDPath, grandchildPIDPath} {
		data, err := waitHooksLiveFile(ctx, process, path)
		if err != nil {
			t.Fatalf("process tree did not publish PID file: %v; artifacts at %s", err, c.artifact)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 1 {
			t.Fatalf("invalid PID in %s; artifacts at %s", filepath.Base(path), c.artifact)
		}
		pids = append(pids, pid)
	}
	defer killHooksLivePIDs(pids)
	control := c.controlCLI("interrupt", "--run", ready.RunID)
	if control.exitCode != 0 || !strings.Contains(control.stdout, "interrupt accepted:") {
		t.Fatalf("run interrupt was not accepted; artifacts at %s", c.artifact)
	}
	if err := process.wait(t); err != nil || process.command.ProcessState.ExitCode() != 0 {
		t.Fatalf("run_js cancellation did not settle; artifacts at %s", c.artifact)
	}
	if err := waitHooksLivePIDsStopped(8*time.Second, pids...); err != nil {
		t.Errorf("owned run_js process tree was not reaped: %v", err)
	}
	rows, err := c.eventRows()
	if err != nil {
		t.Fatal(err)
	}
	started, cancelled := false, false
	for _, event := range rows {
		switch eventString(event, "type") {
		case "run_js.started":
			if eventString(event, "runId") == ready.RunID {
				started = true
			}
		case "run_js.finished":
			if eventString(event, "runId") == ready.RunID && eventString(event, "reason") == "cancelled" {
				cancelled = true
			}
		}
	}
	if !started || !cancelled {
		t.Errorf("run_js start/cancel lifecycle missing: started=%v cancelled=%v", started, cancelled)
	}
	assertHooksLiveGroupWire(t, c, rows, eventStringForRun(rows, ready.RunID, "groupId"), 1)
	c.mu.Lock()
	c.summary["cancelledRunId"] = ready.RunID
	c.summary["ownedPIDs"] = pids
	c.summary["processesStopped"] = waitHooksLivePIDsStopped(200*time.Millisecond, pids...) == nil
	c.mu.Unlock()
	t.Logf("live L3 run_js cancellation artifacts: %s", c.artifact)
}

func waitHooksLivePIDsStopped(timeout time.Duration, pids ...int) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		alive := make([]int, 0, len(pids))
		for _, pid := range pids {
			if exec.Command("kill", "-0", strconv.Itoa(pid)).Run() == nil {
				alive = append(alive, pid)
			}
		}
		if len(alive) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("still alive: %v", alive)
		case <-ticker.C:
		}
	}
}

func killHooksLivePIDs(pids []int) {
	for _, pid := range pids {
		if pid > 1 {
			_ = exec.Command("kill", "-KILL", strconv.Itoa(pid)).Run()
		}
	}
}

func runHooksLiveL4(t *testing.T, artifactRoot, keyFile string) {
	c := newHooksLiveCase(t, artifactRoot, "l4-shared-limit-isolation", keyFile)
	actionsPath, _ := json.Marshal(filepath.Join(c.work, "hook-actions.jsonl"))
	firedPath, _ := json.Marshal(filepath.Join(c.work, "hook-fired.once"))
	c.write("hooks.js", `import {appendFile,writeFile} from "node:fs/promises";
import {existsSync,writeFileSync} from "node:fs";
export async function afterModelResponse(event,api){
  if(existsSync(`+string(firedPath)+`)) return;
  writeFileSync(`+string(firedPath)+`,"started");
  const instruction="Complete the one queued follow-up briefly.";
  const queue1=await api.agents.queue_agent({agentId:event.agentId,instruction,commandId:"hooks-live-limit-queue"});
  const queueReplay=await api.agents.queue_agent({agentId:event.agentId,instruction,commandId:"hooks-live-limit-queue"});
  const steer=await api.agents.steer_agent({runId:event.runId,instruction:"Continue once more with marker LIMIT_STEER_APPLIED." ,commandId:"hooks-live-limit-steer"});
  await appendFile(`+string(actionsPath)+`,JSON.stringify({runId:event.runId,groupId:event.groupId,requestId:event.requestId,queue1,queueReplay,steer})+"\n");
}
export async function beforeTool(event){
  if(event.toolName==="run_js") return {arguments:{code:"// Return a safe notice for an unnecessary tool call.\nconsole.log('No tool was needed.');"}};
}
`)
	main := `import {spawn_agent,run_agent,observe_agent} from "noe:agent";
const aConfig={provider:"opencode-go",model:"deepseek-v4.1-flash",effort:"auto",name:"limit-A",hooks:["./hooks.js"],
  executionLimits:{providerCalls:1,agentStarts:1,queuedTasks:1,hookCalls:20},maxTokens:4096,maxSteps:4};
const bConfig={provider:"opencode-go",model:"deepseek-v4.1-flash",effort:"auto",name:"independent-B",
  executionLimits:{providerCalls:4,agentStarts:1,queuedTasks:0,hookCalls:0},maxTokens:4096,maxSteps:3};
const a=await spawn_agent(aConfig,"Give one short answer. Do not use tools or decision APIs.",{keepAlive:true});
const bPromise=run_agent(bConfig,"Give one short independent answer. Do not use tools or decision APIs.");
const pause=()=>new Promise(resolve=>setTimeout(resolve,20));
let aTerminal;
for(let i=0;i<10000;i++){aTerminal=await observe_agent({agentId:a.agentId});if(aTerminal.state==="stopped")break;await pause();}
if(aTerminal?.state!=="stopped")throw new Error("limited root did not stop");
const b=await bPromise;
console.log(JSON.stringify({aAgentId:a.agentId,aTerminal,bGroupId:b.groupId,bState:b.lastResult?.state,bLastResult:b.lastResult,bExecution:b.execution}));`
	c.write("main.js", main)
	process := c.startAgent("main.js", false)
	if err := process.wait(t); err != nil || process.command.ProcessState.ExitCode() != 0 {
		t.Fatalf("live L4 CLI failed; inspect artifacts at %s", c.artifact)
	}
	output := hooksLiveLastJSONLine(t, c.readOutput("stdout.txt"), c.artifact)
	var report struct {
		AAgentID  string `json:"aAgentId"`
		ATerminal struct {
			State       string `json:"state"`
			Reason      string `json:"reason"`
			GroupID     string `json:"groupId"`
			QueueLength int    `json:"queueLength"`
			Execution   struct {
				Counters struct {
					ProviderCalls int64 `json:"providerCalls"`
					AgentStarts   int64 `json:"agentStarts"`
					QueuedTasks   int64 `json:"queuedTasks"`
					ActiveAgents  int64 `json:"activeAgents"`
					ActiveTasks   int64 `json:"activeTasks"`
				} `json:"counters"`
				Stop *struct {
					Reason    string `json:"reason"`
					LimitName string `json:"limitName"`
					Limit     int64  `json:"limit"`
					Used      int64  `json:"used"`
				} `json:"stop"`
			} `json:"execution"`
		} `json:"aTerminal"`
		BGroupID string `json:"bGroupId"`
		BState   string `json:"bState"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("L4 summary is not JSON; inspect artifacts at %s", c.artifact)
	}
	rows, err := c.eventRows()
	if err != nil {
		t.Fatal(err)
	}
	counters := report.ATerminal.Execution.Counters
	stop := report.ATerminal.Execution.Stop
	if report.AAgentID == "" || report.ATerminal.State != "stopped" || report.ATerminal.Reason != "execution_limit" || report.ATerminal.GroupID != report.AAgentID || stop == nil || stop.LimitName != "providerCalls" || stop.Limit != 1 || stop.Used != 1 {
		t.Errorf("A did not retain provider-call limit stop: %+v", report.ATerminal)
	}
	if counters.ProviderCalls != 1 || counters.AgentStarts != 1 || counters.QueuedTasks != 1 || counters.ActiveAgents != 0 || counters.ActiveTasks != 0 || report.ATerminal.QueueLength != 0 {
		t.Errorf("A shared limit/cancellation counters mismatch: %+v queue=%d", counters, report.ATerminal.QueueLength)
	}
	if report.BGroupID == "" || report.BGroupID == report.AAgentID || report.BState != "completed" {
		t.Errorf("independent root B was affected by A: group=%s state=%s", report.BGroupID, report.BState)
	}
	aModels, bModels, acceptedQueues := 0, 0, 0
	for _, event := range rows {
		if eventString(event, "type") == "model.started" {
			switch eventString(event, "groupId") {
			case report.AAgentID:
				aModels++
			case report.BGroupID:
				bModels++
			}
		}
		if eventString(event, "type") == "command.accepted" && eventString(event, "operation") == "queue" && eventString(event, "agentId") == report.AAgentID && eventString(event, "commandId") == "hooks-live-limit-queue" {
			acceptedQueues++
		}
	}
	if aModels != 1 || bModels == 0 || acceptedQueues != 1 {
		t.Errorf("request/queue isolation mismatch: A models=%d B models=%d accepted queue events=%d", aModels, bModels, acceptedQueues)
	}
	wires := c.recorder.snapshot()
	aWireCount, bWireCount := 0, 0
	for _, wire := range wires {
		if wire.Rejected != "" || wire.Status < 200 || wire.Status >= 300 || wire.Model != hooksLiveModel || wire.ContextNoticeCount < 1 || !wire.ContextNoticeAtTail {
			t.Errorf("invalid authorized request/notice in sequence %d", wire.Sequence)
		}
		if wire.SessionID != "" {
			for _, event := range rows {
				if eventString(event, "type") == "model.started" && eventString(event, "agentId") == wire.SessionID {
					if eventString(event, "groupId") == report.AAgentID {
						aWireCount++
					}
					if eventString(event, "groupId") == report.BGroupID {
						bWireCount++
					}
					break
				}
			}
		}
	}
	if aWireCount != 1 || bWireCount == 0 {
		t.Errorf("real provider request isolation mismatch: A=%d B=%d", aWireCount, bWireCount)
	}
	actions, err := readHooksLiveJSONL(filepath.Join(c.work, "hook-actions.jsonl"))
	if err != nil || len(actions) == 0 {
		t.Errorf("A afterModel hook action receipt missing: %v", err)
	}
	c.mu.Lock()
	c.summary["aGroupProviderStarts"] = aModels
	c.summary["bGroupProviderStarts"] = bModels
	c.summary["aAcceptedQueueEvents"] = acceptedQueues
	c.summary["aLimitStop"] = stop
	c.mu.Unlock()
	t.Logf("live L4 artifacts: %s", c.artifact)
}
