//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type consoleModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
}

type consoleSnapshot struct {
	AgentID          string                     `json:"agentId"`
	State            string                     `json:"state"`
	CurrentRunID     string                     `json:"currentRunId"`
	CurrentCommandID string                     `json:"currentCommandId"`
	QueueLength      int                        `json:"queueLength"`
	KeepAlive        bool                       `json:"keepAlive"`
	LastResult       map[string]json.RawMessage `json:"lastResult"`
}

type consoleState struct {
	SessionID    string          `json:"sessionId"`
	Model        consoleModel    `json:"model"`
	Snapshot     consoleSnapshot `json:"snapshot"`
	Error        json.RawMessage `json:"error"`
	WindowSize   int             `json:"windowSize"`
	PollInterval int             `json:"pollInterval"`
}

type consoleReceipt struct {
	CommandID string `json:"commandId"`
	AgentID   string `json:"agentId"`
	Status    string `json:"status"`
}

type consoleTarget struct {
	AgentID   string `json:"agentId"`
	RunID     string `json:"runId"`
	CommandID string `json:"commandId"`
}

type consoleRecord struct {
	Target         consoleTarget   `json:"target"`
	Seq            int64           `json:"seq"`
	Time           string          `json:"time"`
	Type           string          `json:"type"`
	Snapshot       consoleSnapshot `json:"snapshot"`
	Operation      string          `json:"operation"`
	Instruction    string          `json:"instruction"`
	AgentID        string          `json:"agentId"`
	RunID          string          `json:"runId"`
	CommandID      string          `json:"commandId"`
	Receipt        *consoleReceipt `json:"receipt"`
	Error          json.RawMessage `json:"error"`
	State          string          `json:"state"`
	Answer         string          `json:"answer"`
	ResultRecorded *bool           `json:"resultRecorded"`
}

type consoleHistory struct {
	SessionID string          `json:"sessionId"`
	Records   []consoleRecord `json:"records"`
	FirstSeq  int64           `json:"firstSeq"`
	LastSeq   int64           `json:"lastSeq"`
	Total     int             `json:"total"`
}

type consoleOperation struct {
	Receipt consoleReceipt  `json:"receipt"`
	Record  consoleRecord   `json:"record"`
	Error   json.RawMessage `json:"error"`
}

// The real sample owns its monitor and agent; no replacement entry point or
// injected console internals are used by these tests.
type consoleCLI struct {
	cmd            *exec.Cmd
	stdout, stderr *synchronizedCapture
	done           chan struct{}
	err            error
	url            string
	client         *http.Client
}

func startConsoleCLI(t *testing.T, dataDir string, env map[string]string) *consoleCLI {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	cmd := exec.CommandContext(ctx, cliPath, "run", filepath.Join(repoRootPath, "examples", "agent-console", "main.js"),
		"--provider", "openrouter", "--model", "fixture/model", "--effort", "auto", "--port", "0", "--data-dir", dataDir)
	cmd.Dir, cmd.Env = t.TempDir(), os.Environ()
	for key, value := range env {
		cmd.Env = envWith(cmd.Env, key, value)
	}
	process := &consoleCLI{cmd: cmd, stdout: newSynchronizedCapture(), stderr: newSynchronizedCapture(), done: make(chan struct{}), client: &http.Client{Timeout: 3 * time.Second}}
	cmd.Stdout, cmd.Stderr = process.stdout, process.stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	go func() { process.err = cmd.Wait(); close(process.done) }()
	t.Cleanup(func() {
		process.client.CloseIdleConnections()
		select {
		case <-process.done:
		default:
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-process.done:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-process.done
			}
		}
		cancel()
	})
	consoleEventually(t, "console startup URL", func() bool {
		for _, line := range strings.Split(process.stdout.String(), "\n") {
			const prefix = "NOE Agent Console: "
			if strings.HasPrefix(line, prefix) {
				address := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				parsed, err := url.Parse(address)
				if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.Port() == "0" {
					t.Fatalf("invalid console URL %q", address)
				}
				process.url = strings.TrimRight(address, "/")
				return true
			}
		}
		select {
		case <-process.done:
			t.Fatalf("console exited during startup: %v\nstdout: %s\nstderr: %s", process.err, process.stdout.String(), process.stderr.String())
		default:
		}
		return false
	})
	return process
}

func consoleEventually(t *testing.T, description string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		if ready() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (p *consoleCLI) request(t *testing.T, method, path string, payload any, origin *string, host string) (int, []byte) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, p.url+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != nil {
		req.Header.Set("Origin", *origin)
	}
	if host != "" {
		req.Host = host
	}
	response, err := p.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v\nstdout: %s\nstderr: %s", method, path, err, p.stdout.String(), p.stderr.String())
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data
}

func consoleDecode(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode console response %q: %v", data, err)
	}
}

func (p *consoleCLI) get(t *testing.T, path string, target any) {
	t.Helper()
	status, data := p.request(t, http.MethodGet, path, nil, nil, "")
	if status != http.StatusOK {
		t.Fatalf("GET %s: status=%d body=%s", path, status, data)
	}
	consoleDecode(t, data, target)
}

func (p *consoleCLI) state(t *testing.T) consoleState {
	t.Helper()
	var state consoleState
	p.get(t, "/api/state", &state)
	return state
}

func (p *consoleCLI) awaitState(t *testing.T, description string, predicate func(consoleSnapshot) bool) consoleState {
	t.Helper()
	var state consoleState
	consoleEventually(t, description, func() bool { state = p.state(t); return predicate(state.Snapshot) })
	return state
}

func (p *consoleCLI) history(t *testing.T, session string) consoleHistory {
	t.Helper()
	var history consoleHistory
	p.get(t, "/api/history?session="+url.QueryEscape(session)+"&limit=1000", &history)
	if history.SessionID != session {
		t.Fatalf("history changed session: got=%q want=%q", history.SessionID, session)
	}
	var seq int64
	for _, record := range history.Records {
		if record.Seq <= seq || record.Time == "" || record.Type == "" {
			t.Fatalf("unordered or unlabelled history record: %+v after %d", record, seq)
		}
		seq = record.Seq
	}
	return history
}

func (p *consoleCLI) operate(t *testing.T, operation string, payload any) consoleOperation {
	t.Helper()
	status, data := p.request(t, http.MethodPost, "/api/"+operation, payload, &p.url, "")
	var result consoleOperation
	consoleDecode(t, data, &result)
	if status < 200 || status >= 300 || consoleHasError(result.Error) || result.Receipt.Status != "accepted" || result.Record.Type != "operation" || result.Record.Operation != operation || result.Record.Receipt == nil {
		t.Fatalf("%s acceptance: status=%d body=%s", operation, status, data)
	}
	return result
}

func consoleHasError(value json.RawMessage) bool {
	return len(value) > 0 && string(value) != "null" && string(value) != `""`
}

func (p *consoleCLI) queue(t *testing.T, session, instruction, command string) consoleOperation {
	t.Helper()
	payload := map[string]any{"sessionId": session, "instruction": instruction}
	if command != "" {
		payload["commandId"] = command
	}
	result := p.operate(t, "queue", payload)
	if result.Receipt.CommandID == "" || (command != "" && result.Receipt.CommandID != command) || result.Record.CommandID != result.Receipt.CommandID || result.Record.Instruction != instruction {
		t.Fatalf("queue lost command/instruction: %+v", result)
	}
	return result
}

func (p *consoleCLI) awaitResults(t *testing.T, session string, expected map[string]string) consoleHistory {
	t.Helper()
	var history consoleHistory
	consoleEventually(t, fmt.Sprintf("results for %v", expected), func() bool {
		history = p.history(t, session)
		found := map[string]bool{}
		for _, record := range history.Records {
			if record.Type != "result" {
				continue
			}
			if state, ok := expected[record.CommandID]; ok {
				if found[record.CommandID] || record.State != state {
					t.Fatalf("duplicate or incorrect result: %+v", record)
				}
				found[record.CommandID] = true
			}
		}
		return len(found) == len(expected)
	})
	return history
}

func consoleLastUser(request agentChatRequest) string {
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role == "user" {
			var text string
			_ = json.Unmarshal(request.Messages[i].Content, &text)
			return text
		}
	}
	return ""
}

func consoleTranscript(request agentChatRequest) []string {
	var messages []string
	for _, message := range request.Messages {
		if message.Role != "system" {
			var text string
			_ = json.Unmarshal(message.Content, &text)
			messages = append(messages, message.Role+":"+text)
		}
	}
	return messages
}

func consoleAwaitRequest(t *testing.T, requests <-chan agentChatRequest, description string) agentChatRequest {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(8 * time.Second):
		t.Fatalf("provider did not receive %s", description)
		return agentChatRequest{}
	}
}

func consoleReject(t *testing.T, status int, data []byte) {
	t.Helper()
	var result struct {
		Error json.RawMessage `json:"error"`
	}
	consoleDecode(t, data, &result)
	if status < 400 || status >= 500 || !consoleHasError(result.Error) {
		t.Fatalf("expected client rejection: status=%d body=%s", status, data)
	}
}

// Plan paths 1, 2 and 9: an idle real sample, full Unicode answers, fast
// command-specific result collection, continuing conversation, and local-only HTTP.
func TestAgentConsoleIdleResultsAndSecurity(t *testing.T) {
	const firstInstruction = "preserve the entire Unicode response\n  **literal instruction**"
	longAnswer := strings.Repeat("日本語🌱/é/", 700) + "\n終端 sentinel 🌌"
	if len(longAnswer) <= 4096 {
		t.Fatal("fixture answer must cross read_task_result's byte page")
	}
	var calls atomic.Int32
	requests := make(chan agentChatRequest, 8)
	provider := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
		calls.Add(1)
		requests <- request
		if request.Model != "fixture/model" || agentEffort(request) != "auto" {
			t.Errorf("console model settings: %+v", request)
		}
		switch consoleLastUser(request) {
		case firstInstruction:
			return agentModelReply{Content: longAnswer}
		case "fast second":
			return agentModelReply{Content: "second answer"}
		case "fast third":
			return agentModelReply{Content: "third answer"}
		default:
			t.Errorf("unexpected console instruction: %q", consoleLastUser(request))
			return agentModelReply{Content: "unexpected"}
		}
	})
	t.Cleanup(provider.Close)
	env := agentCLIEnv(t, provider.URL)
	env["NOE_AGENT_TRACE"] = ""
	process := startConsoleCLI(t, t.TempDir(), env)
	state := process.awaitState(t, "initial idle snapshot", func(s consoleSnapshot) bool { return s.State == "idle" })
	if state.SessionID == "" || state.Snapshot.AgentID == "" || !state.Snapshot.KeepAlive || state.Snapshot.CurrentRunID != "" || state.Snapshot.QueueLength != 0 || state.Model != (consoleModel{"openrouter", "fixture/model", "auto"}) || state.WindowSize != 1000 || state.PollInterval != 1000 || consoleHasError(state.Error) {
		t.Fatalf("startup state: %+v", state)
	}
	for _, path := range []string{"/", "/app.js"} {
		status, data := process.request(t, http.MethodGet, path, nil, nil, "")
		if status != 200 || len(data) == 0 || bytes.Contains(data, []byte("agent-fixture-secret")) {
			t.Fatalf("static %s: status=%d body=%q", path, status, data)
		}
	}
	// Wait through a normal monitor tick: spawning/observing idle must not infer.
	time.Sleep(time.Duration(state.PollInterval+100) * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("idle startup called provider %d times", calls.Load())
	}
	for _, path := range []string{"/", "/app.js", "/api/state", "/api/sessions", "/api/history?session=" + url.QueryEscape(state.SessionID)} {
		status, data := process.request(t, http.MethodGet, path, nil, nil, "foreign.invalid:1234")
		consoleReject(t, status, data)
	}
	for _, origin := range []*string{nil, consoleString("null"), consoleString("http://foreign.invalid:1234")} {
		status, data := process.request(t, http.MethodPost, "/api/queue", map[string]any{"sessionId": state.SessionID, "instruction": "must not run"}, origin, "")
		consoleReject(t, status, data)
	}
	foreign := "http://foreign.invalid:1234"
	status, data := process.request(t, http.MethodPost, "/api/queue", map[string]any{"sessionId": state.SessionID, "instruction": "must not run"}, &foreign, "foreign.invalid:1234")
	consoleReject(t, status, data)
	if calls.Load() != 0 {
		t.Fatalf("rejected requests called provider %d times", calls.Load())
	}
	first := process.queue(t, state.SessionID, firstInstruction, "console-long")
	second := process.queue(t, state.SessionID, "fast second", "")
	third := process.queue(t, state.SessionID, "fast third", "")
	expected := map[string]string{first.Receipt.CommandID: longAnswer, second.Receipt.CommandID: "second answer", third.Receipt.CommandID: "third answer"}
	history := process.awaitResults(t, state.SessionID, map[string]string{first.Receipt.CommandID: "completed", second.Receipt.CommandID: "completed", third.Receipt.CommandID: "completed"})
	runs := map[string]bool{}
	operations := map[string]bool{}
	for _, record := range history.Records {
		switch record.Type {
		case "operation":
			if _, ok := expected[record.CommandID]; ok && record.Operation == "queue" && record.Receipt != nil {
				operations[record.CommandID] = true
			}
		case "result":
			if answer, ok := expected[record.CommandID]; ok {
				if record.Answer != answer || record.RunID == "" || runs[record.RunID] || consoleHasError(record.Error) {
					t.Fatalf("lost/full result correlation: command=%s bytes=%d want=%d run=%s", record.CommandID, len(record.Answer), len(answer), record.RunID)
				}
				runs[record.RunID] = true
			}
		case "observe":
			if _, ok := record.Snapshot.LastResult["answer"]; ok {
				t.Fatal("observe duplicated an answer instead of includeAnswer:false")
			}
		}
	}
	if len(runs) != 3 || len(operations) != 3 {
		t.Fatalf("command history: runs=%v operations=%v", runs, operations)
	}
	for index := 0; index < 3; index++ {
		request := consoleAwaitRequest(t, requests, "queued request")
		if request.SessionID != state.Snapshot.AgentID {
			t.Fatalf("queue changed conversation identity: %q != %q", request.SessionID, state.Snapshot.AgentID)
		}
		transcript := consoleTranscript(request)
		want := []string{"user:" + firstInstruction}
		if index >= 1 {
			want = append(want, "assistant:"+longAnswer, "user:fast second")
		}
		if index >= 2 {
			want = append(want, "assistant:second answer", "user:fast third")
		}
		if strings.Join(transcript, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("queue %d did not retain the conversation (got %d messages, want %d)", index, len(transcript), len(want))
		}
	}
	process.awaitState(t, "idle after queue", func(s consoleSnapshot) bool { return s.State == "idle" && s.CurrentRunID == "" })
	if calls.Load() != 3 {
		t.Fatalf("provider calls=%d, want exactly three", calls.Load())
	}
}

func consoleString(value string) *string { return &value }

// Plan paths 3 and 4: steer is a receipt at the frozen run, interrupt keeps the
// next queue alive, stale controls cannot retarget, and stop cancels all work.
func TestAgentConsoleSteerInterruptAndStop(t *testing.T) {
	const steer = "correction\n\n  **preserve this exact steer**"
	firstRelease, survivorRelease := make(chan struct{}), make(chan struct{})
	firstRequests, steeredRequests := make(chan agentChatRequest, 2), make(chan agentChatRequest, 2)
	interruptRequests, survivorRequests, stopRequests := make(chan agentChatRequest, 2), make(chan agentChatRequest, 2), make(chan agentChatRequest, 2)
	interruptCancelled, survivorCancelled, stopCancelled := make(chan struct{}, 1), make(chan struct{}, 1), make(chan struct{}, 1)
	var calls atomic.Int32
	provider := agentAPIFixture(t, func(ctx context.Context, request agentChatRequest) agentModelReply {
		calls.Add(1)
		switch consoleLastUser(request) {
		case "first task":
			firstRequests <- request
			select {
			case <-firstRelease:
				return agentModelReply{Content: "candidate"}
			case <-ctx.Done():
				return agentModelReply{Status: 500}
			}
		case steer:
			steeredRequests <- request
			return agentModelReply{Content: "corrected answer"}
		case "interrupt me":
			interruptRequests <- request
			<-ctx.Done()
			interruptCancelled <- struct{}{}
			return agentModelReply{Status: 500}
		case "survive interrupt":
			survivorRequests <- request
			select {
			case <-survivorRelease:
				return agentModelReply{Content: "survivor answer"}
			case <-ctx.Done():
				survivorCancelled <- struct{}{}
				return agentModelReply{Status: 500}
			}
		case "stop active":
			stopRequests <- request
			<-ctx.Done()
			stopCancelled <- struct{}{}
			return agentModelReply{Status: 500}
		default:
			t.Errorf("unexpected provider request after control: %q", consoleLastUser(request))
			return agentModelReply{Content: "unexpected"}
		}
	})
	t.Cleanup(provider.Close)
	env := agentCLIEnv(t, provider.URL)
	env["NOE_AGENT_TRACE"] = ""
	process := startConsoleCLI(t, t.TempDir(), env)
	initial := process.state(t)
	first := process.queue(t, initial.SessionID, "first task", "first")
	consoleAwaitRequest(t, firstRequests, "first task")
	active := process.awaitState(t, "first active run", func(s consoleSnapshot) bool {
		return s.CurrentCommandID == first.Receipt.CommandID && s.CurrentRunID != ""
	})
	second := process.queue(t, initial.SessionID, "interrupt me", "second")
	process.awaitState(t, "queued successor", func(s consoleSnapshot) bool {
		return s.CurrentRunID == active.Snapshot.CurrentRunID && s.QueueLength == 1
	})
	accepted := process.operate(t, "steer", map[string]any{"sessionId": initial.SessionID, "instruction": steer, "runId": active.Snapshot.CurrentRunID, "commandId": "steer-first"})
	if accepted.Receipt.CommandID != "steer-first" || accepted.Record.Target.RunID != active.Snapshot.CurrentRunID || accepted.Record.Instruction != steer {
		t.Fatalf("steer lost frozen target: %+v", accepted)
	}
	if calls.Load() != 1 {
		t.Fatalf("steer receipt unexpectedly interrupted/restarted provider: calls=%d", calls.Load())
	}
	close(firstRelease)
	corrected := consoleAwaitRequest(t, steeredRequests, "steer at model boundary")
	if got := strings.Join(consoleTranscript(corrected), "|"); got != "user:first task|assistant:candidate|user:"+steer {
		t.Fatalf("steer transcript: %s", got)
	}
	consoleAwaitRequest(t, interruptRequests, "queued interrupt target")
	secondActive := process.awaitState(t, "second active run", func(s consoleSnapshot) bool {
		return s.CurrentCommandID == second.Receipt.CommandID && s.CurrentRunID != ""
	})
	third := process.queue(t, initial.SessionID, "survive interrupt", "third")
	interrupted := process.operate(t, "interrupt", map[string]any{"sessionId": initial.SessionID, "commandId": second.Receipt.CommandID})
	if interrupted.Receipt.CommandID != second.Receipt.CommandID || interrupted.Record.Target.CommandID != second.Receipt.CommandID {
		t.Fatalf("interrupt changed target: %+v", interrupted)
	}
	select {
	case <-interruptCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("interrupt did not cancel its provider request")
	}
	resumed := consoleAwaitRequest(t, survivorRequests, "surviving queue")
	if resumed.SessionID != initial.Snapshot.AgentID {
		t.Fatal("interrupt replaced the agent")
	}
	thirdActive := process.awaitState(t, "survivor active run", func(s consoleSnapshot) bool {
		return s.CurrentCommandID == third.Receipt.CommandID && s.CurrentRunID != ""
	})
	if thirdActive.Snapshot.CurrentRunID == secondActive.Snapshot.CurrentRunID {
		t.Fatal("new task reused old run ID")
	}
	for _, control := range []struct {
		operation string
		payload   map[string]any
	}{
		{"steer", map[string]any{"sessionId": initial.SessionID, "runId": secondActive.Snapshot.CurrentRunID, "instruction": "stale steer"}},
		{"interrupt", map[string]any{"sessionId": initial.SessionID, "commandId": second.Receipt.CommandID}},
	} {
		status, data := process.request(t, http.MethodPost, "/api/"+control.operation, control.payload, &process.url, "")
		consoleReject(t, status, data)
	}
	afterStale := process.state(t)
	if afterStale.Snapshot.CurrentCommandID != third.Receipt.CommandID || afterStale.Snapshot.CurrentRunID != thirdActive.Snapshot.CurrentRunID {
		t.Fatalf("stale control hit successor: %+v", afterStale)
	}
	select {
	case <-survivorCancelled:
		t.Fatal("stale interrupt cancelled the successor")
	default:
	}
	close(survivorRelease)
	history := process.awaitResults(t, initial.SessionID, map[string]string{first.Receipt.CommandID: "completed", second.Receipt.CommandID: "cancelled", third.Receipt.CommandID: "completed"})
	for _, record := range history.Records {
		if record.Type == "result" && record.CommandID == first.Receipt.CommandID && record.Answer != "corrected answer" {
			t.Fatalf("steer result: %+v", record)
		}
	}
	fourth := process.queue(t, initial.SessionID, "stop active", "fourth")
	consoleAwaitRequest(t, stopRequests, "stop target")
	process.awaitState(t, "stop active snapshot", func(s consoleSnapshot) bool {
		return s.CurrentCommandID == fourth.Receipt.CommandID && s.CurrentRunID != ""
	})
	fifth := process.queue(t, initial.SessionID, "must never start", "fifth")
	process.awaitState(t, "stop queued successor", func(s consoleSnapshot) bool { return s.QueueLength == 1 })
	process.operate(t, "stop", map[string]any{"sessionId": initial.SessionID})
	select {
	case <-stopCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not cancel active provider request")
	}
	process.awaitResults(t, initial.SessionID, map[string]string{fourth.Receipt.CommandID: "cancelled", fifth.Receipt.CommandID: "cancelled"})
	process.awaitState(t, "stopped agent and empty queue", func(s consoleSnapshot) bool {
		return s.State == "stopped" && s.CurrentRunID == "" && s.QueueLength == 0
	})
	if calls.Load() != 5 {
		t.Fatalf("stop/stale controls ran unintended work: calls=%d want=5", calls.Load())
	}
}

// Read only the sample's JSONL data root, independently of HTTP. This proves
// monitoring persisted changes while no browser was connected, rather than an
// API read accidentally driving result collection.
func consoleDiskRecords(t *testing.T, root string) []consoleRecord {
	t.Helper()
	var records []consoleRecord
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range bytes.Split(data, []byte("\n")) {
			var record consoleRecord
			if json.Unmarshal(line, &record) == nil && record.Seq > 0 {
				records = append(records, record)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return records
}

// Plan path 7: closing/reopening HTTP clients does not own agent or monitor life.
func TestAgentConsoleDisconnectedClientKeepsMonitoring(t *testing.T) {
	requests := make(chan agentChatRequest, 1)
	release := make(chan struct{})
	provider := agentAPIFixture(t, func(ctx context.Context, request agentChatRequest) agentModelReply {
		requests <- request
		select {
		case <-release:
			return agentModelReply{Content: "completed without a browser 🌱"}
		case <-ctx.Done():
			return agentModelReply{Status: 500}
		}
	})
	t.Cleanup(provider.Close)
	env := agentCLIEnv(t, provider.URL)
	env["NOE_AGENT_TRACE"] = ""
	dataDir := t.TempDir()
	process := startConsoleCLI(t, dataDir, env)
	initial := process.state(t)
	queued := process.queue(t, initial.SessionID, "continue offline", "offline")
	consoleAwaitRequest(t, requests, "offline task")
	process.awaitState(t, "offline running snapshot", func(s consoleSnapshot) bool {
		return s.CurrentCommandID == queued.Receipt.CommandID && s.CurrentRunID != ""
	})
	before := process.history(t, initial.SessionID)
	process.client.CloseIdleConnections()
	close(release)
	consoleEventually(t, "result and idle observation saved without HTTP clients", func() bool {
		result, idle := false, false
		for _, record := range consoleDiskRecords(t, dataDir) {
			if record.Seq <= before.LastSeq {
				continue
			}
			result = result || (record.Type == "result" && record.CommandID == queued.Receipt.CommandID && record.Answer == "completed without a browser 🌱")
			idle = idle || (record.Type == "observe" && record.Snapshot.State == "idle")
		}
		return result && idle
	})
	process.client = &http.Client{Timeout: 3 * time.Second}
	var delta consoleHistory
	process.get(t, fmt.Sprintf("/api/history?session=%s&limit=1000&after=%d", url.QueryEscape(initial.SessionID), before.LastSeq), &delta)
	found := 0
	for _, record := range delta.Records {
		if record.Seq <= before.LastSeq {
			t.Fatalf("reconnect repeated old record: %+v", record)
		}
		if record.Type == "result" && record.CommandID == queued.Receipt.CommandID {
			found++
			if record.Answer != "completed without a browser 🌱" {
				t.Fatalf("reconnected answer: %+v", record)
			}
		}
	}
	if delta.SessionID != initial.SessionID || found != 1 || process.state(t).SessionID != initial.SessionID {
		t.Fatalf("reconnect did not retain session/result: %+v", delta)
	}
}

// Plan path 8: an abrupt owner exit deliberately leaves accepted operations
// without results. Restart opens them read-only and creates a fresh idle agent,
// never re-executing old accepted or queued work.
func TestAgentConsoleRestartReadsHistoryWithoutReplayingQueue(t *testing.T) {
	requests := make(chan agentChatRequest, 8)
	cancelled := make(chan struct{}, 1)
	var calls atomic.Int32
	provider := agentAPIFixture(t, func(ctx context.Context, request agentChatRequest) agentModelReply {
		calls.Add(1)
		requests <- request
		switch consoleLastUser(request) {
		case "saved answer":
			return agentModelReply{Content: "durable answer 🌌"}
		case "unfinished active":
			<-ctx.Done()
			cancelled <- struct{}{}
			return agentModelReply{Status: 500}
		case "fresh session":
			return agentModelReply{Content: "fresh answer"}
		default:
			t.Errorf("old queue replayed/unexpected request: %q", consoleLastUser(request))
			return agentModelReply{Content: "unexpected replay"}
		}
	})
	t.Cleanup(provider.Close)
	env := agentCLIEnv(t, provider.URL)
	env["NOE_AGENT_TRACE"] = ""
	dataDir := t.TempDir()
	first := startConsoleCLI(t, dataDir, env)
	old := first.state(t)
	saved := first.queue(t, old.SessionID, "saved answer", "saved")
	consoleAwaitRequest(t, requests, "saved answer")
	first.awaitResults(t, old.SessionID, map[string]string{saved.Receipt.CommandID: "completed"})
	unfinished := first.queue(t, old.SessionID, "unfinished active", "unfinished")
	consoleAwaitRequest(t, requests, "unfinished active")
	first.awaitState(t, "running snapshot before owner exit", func(s consoleSnapshot) bool {
		return s.CurrentCommandID == unfinished.Receipt.CommandID && s.State == "running"
	})
	queued := first.queue(t, old.SessionID, "unfinished queued", "unstarted")
	// SIGKILL, unlike normal stop, cannot collect terminal results before exit.
	if err := first.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.done:
	case <-time.After(5 * time.Second):
		t.Fatal("console process did not exit")
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("owner exit left its provider request alive")
	}
	second := startConsoleCLI(t, dataDir, env)
	current := second.awaitState(t, "fresh idle session after restart", func(s consoleSnapshot) bool { return s.State == "idle" })
	if current.SessionID == old.SessionID || current.Snapshot.AgentID == old.Snapshot.AgentID {
		t.Fatalf("restart reused old live identity: old=%+v new=%+v", old, current)
	}
	var sessions struct {
		CurrentSessionID string `json:"currentSessionId"`
		Sessions         []struct {
			SessionID string       `json:"sessionId"`
			StartedAt string       `json:"startedAt"`
			AgentID   string       `json:"agentId"`
			Name      string       `json:"name"`
			Model     consoleModel `json:"model"`
			Current   bool         `json:"current"`
		} `json:"sessions"`
	}
	second.get(t, "/api/sessions", &sessions)
	if sessions.CurrentSessionID != current.SessionID || len(sessions.Sessions) != 2 {
		t.Fatalf("session listing: %+v", sessions)
	}
	for _, session := range sessions.Sessions {
		if session.StartedAt == "" || session.Name == "" || session.Model != current.Model {
			t.Fatalf("missing historical metadata: %+v", session)
		}
		switch session.SessionID {
		case old.SessionID:
			if session.Current || session.AgentID != old.Snapshot.AgentID {
				t.Fatalf("old session reported live: %+v", session)
			}
		case current.SessionID:
			if !session.Current || session.AgentID != current.Snapshot.AgentID {
				t.Fatalf("new session metadata: %+v", session)
			}
		default:
			t.Fatalf("unknown session: %+v", session)
		}
	}
	history := second.history(t, old.SessionID)
	flags := map[string]bool{}
	savedAnswer, running := false, false
	for _, record := range history.Records {
		if record.Type == "operation" && record.Operation == "queue" && record.Receipt != nil {
			if record.ResultRecorded == nil {
				t.Fatalf("historical queue missing resultRecorded: %+v", record)
			}
			flags[record.CommandID] = *record.ResultRecorded
		}
		savedAnswer = savedAnswer || (record.Type == "result" && record.CommandID == saved.Receipt.CommandID && record.Answer == "durable answer 🌌")
		running = running || (record.Type == "observe" && record.Snapshot.State == "running" && record.Snapshot.CurrentCommandID == unfinished.Receipt.CommandID)
	}
	if len(flags) != 3 || !flags[saved.Receipt.CommandID] || flags[unfinished.Receipt.CommandID] || flags[queued.Receipt.CommandID] || !savedAnswer || !running {
		t.Fatalf("restart lost historical results/unresolved receipts: flags=%v saved=%v running=%v", flags, savedAnswer, running)
	}
	status, data := second.request(t, http.MethodPost, "/api/queue", map[string]any{"sessionId": old.SessionID, "instruction": "must not retarget old session"}, &second.url, "")
	consoleReject(t, status, data)
	// Include a normal monitor interval before proving no previous queue resumed.
	time.Sleep(time.Duration(current.PollInterval+100) * time.Millisecond)
	if calls.Load() != 2 {
		t.Fatalf("restart re-executed prior queue: provider calls=%d", calls.Load())
	}
	fresh := second.queue(t, current.SessionID, "fresh session", "")
	request := consoleAwaitRequest(t, requests, "fresh session")
	if got := strings.Join(consoleTranscript(request), "|"); got != "user:fresh session" || request.SessionID != current.Snapshot.AgentID {
		t.Fatalf("restart resumed previous conversation: %v session=%q", consoleTranscript(request), request.SessionID)
	}
	second.awaitResults(t, current.SessionID, map[string]string{fresh.Receipt.CommandID: "completed"})
	if calls.Load() != 3 {
		t.Fatalf("unexpected replay after fresh queue: calls=%d", calls.Load())
	}
}

// Keep the focused storage/controller checks in the standard E2E entry point.
func TestAgentConsoleBackendScript(t *testing.T) {
	result := runCLI(t, t.TempDir(), nil, "run", filepath.Join(repoRootPath, "examples", "agent-console", "tests", "backend.js"))
	requireExit(t, result, 0)
	if !strings.Contains(result.stdout, "Agent console backend tests passed:") || result.stderr != "" {
		t.Fatalf("console backend script: stdout=%q stderr=%q", result.stdout, result.stderr)
	}
}
