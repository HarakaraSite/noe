package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"noe/internal/events"
)

type observeCapture struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	changed chan struct{}
}

func newObserveCapture() *observeCapture { return &observeCapture{changed: make(chan struct{}, 1)} }
func (w *observeCapture) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buf.Write(p)
	w.mu.Unlock()
	select {
	case w.changed <- struct{}{}:
	default:
	}
	return n, err
}
func (w *observeCapture) text() string { w.mu.Lock(); defer w.mu.Unlock(); return w.buf.String() }
func (w *observeCapture) wait(t *testing.T, text string) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for !strings.Contains(w.text(), text) {
		select {
		case <-w.changed:
		case <-timer.C:
			t.Fatalf("missing %q in %q", text, w.text())
		}
	}
}

func TestObserveHumanStreamsReadableThinkingAndAcceptedMessages(t *testing.T) {
	input, producer := io.Pipe()
	defer input.Close()
	defer producer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := newObserveCapture()
	var diagnostic bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"observe"}, input, out, &diagnostic) }()
	write := func(event events.Event) {
		t.Helper()
		if err := json.NewEncoder(producer).Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	base := events.Event{"executionId": "exec", "runId": "run-A", "name": "parent-A", "requestId": "m1"}
	emit := func(kind, text string) {
		event := events.Event{}
		for k, v := range base {
			event[k] = v
		}
		event["type"], event["text"] = kind, text
		write(event)
	}
	emit("thinking", "考え")
	emit("thinking", "ています")
	out.wait(t, "[parent-A/run-A] thinking: 考えています") // Producer is still open.
	emit("thinking", "\n\n次に")
	emit("thinking", "確認します。\n")
	out.wait(t, "thinking: 考えています\n\n  次に確認します。\n")
	emit("message_delta", "途中の発言")
	emit("agent.message", "途中の発言")
	write(events.Event{"type": "run_js.started", "executionId": "exec", "runId": "run-A", "name": "parent-A", "toolCallId": "tool", "text": "// READMEを読む"})
	write(events.Event{"type": "run_js.finished", "executionId": "exec", "runId": "run-A", "name": "parent-A", "toolCallId": "tool", "success": true, "exitCode": 0, "duration": 28000000, "reason": "completed"})
	emit("agent.final", "最終報告です")
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d: %s", code, &diagnostic)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("observer did not finish at EOF")
	}
	text := out.text()
	if strings.Count(text, "途中の発言") != 1 || !strings.Contains(text, "run_js started: READMEを読む") || !strings.Contains(text, "run_js succeeded: 28ms") || !strings.Contains(text, "final answer\n  最終報告です") {
		t.Fatalf("unreadable or duplicate output:\n%s", text)
	}
	if strings.Count(text, "] thinking:") != 1 {
		t.Fatalf("repeated thinking label:\n%s", text)
	}
}

func TestObserveHumanGroupsThinkingAcrossLinesButKeepsStreamsSeparate(t *testing.T) {
	var out bytes.Buffer
	renderer := newAgentRenderer(&out)
	emit := func(name, runID, requestID, kind, text string) {
		t.Helper()
		if err := renderer.render(events.Event{"executionId": "exec", "name": name, "runId": runID, "requestId": requestID, "type": kind, "text": text}); err != nil {
			t.Fatal(err)
		}
	}
	emit("A", "run-A", "m1", "thinking", "First")
	emit("A", "run-A", "m1", "thinking", "\n\nSecond\nThird")
	emit("A", "run-A", "m1", "stdout", "hidden")
	emit("A", "run-A", "m1", "thinking", "\n")
	emit("A", "run-A", "m1", "thinking_summary", "Summary\n")
	emit("A", "run-A", "m1", "thinking_summary", "More")
	emit("B", "run-B", "m1", "thinking", "Other\n")
	emit("A", "run-A", "m1", "thinking", "Back\n")
	emit("A", "run-A", "m2", "thinking", "Next request\n")
	emit("A", "run-A", "m2", "run_js.started", "// Read file")
	emit("A", "run-A", "m2", "thinking", "After tool")
	if err := renderer.finish(); err != nil {
		t.Fatal(err)
	}
	want := "[A/run-A] thinking: First\n\n  Second\n  Third\n" +
		"[A/run-A] thinking summary: Summary\n  More\n" +
		"[B/run-B] thinking: Other\n" +
		"[A/run-A] thinking: Back\n" +
		"[A/run-A] thinking: Next request\n" +
		"[A/run-A] run_js started: Read file\n" +
		"[A/run-A] thinking: After tool\n"
	if got := out.String(); got != want {
		t.Fatalf("thinking groups:\n%q\nwant:\n%q", got, want)
	}
}

func TestObserveFollowHoldsIncompleteRowAndCancels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := io.WriteString(file, "{\"executionId\":\"exec\",\"runId\":\"run-A\",\"name\":\"parent-A\",\"type\":\"agent.started\"}\n{\"executionId\":\"exec\",\"runId\":\"run-A\","); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := newObserveCapture()
	var diagnostic bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"observe", "--follow", path}, strings.NewReader(""), out, &diagnostic)
	}()
	out.wait(t, "[parent-A/run-A] started")
	if strings.Contains(out.text(), "run_js started") {
		t.Fatal("partial row was consumed")
	}
	if _, err := io.WriteString(file, "\"name\":\"parent-A\",\"type\":\"run_js.started\",\"text\":\"// 途中の行\"}\n"); err != nil {
		t.Fatal(err)
	}
	out.wait(t, "run_js started: 途中の行")
	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("exit %d: %s", code, &diagnostic)
		}
	case <-time.After(time.Second):
		t.Fatal("follow did not cancel")
	}
}

func TestObserveJSONReadsRecordedRun(t *testing.T) {
	path := filepath.Join("..", "observation", "testdata", "real-run.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event events.Event
		if json.Unmarshal(line, &event) == nil && event["type"] == "agent.started" {
			runID = eventString(event, "runId")
			break
		}
	}
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), []string{"observe", "--json", "--run", runID, "--wait", path}, strings.NewReader(""), &out, &diagnostic)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, &diagnostic)
	}
	var snapshot struct {
		Status, FinalText string
		Events            []events.Event
	}
	if err := json.Unmarshal(out.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "completed" || !strings.Contains(snapshot.FinalText, "5050") {
		t.Fatalf("snapshot: %s", &out)
	}
	for _, event := range snapshot.Events {
		if event["type"] == "thinking" || event["type"] == "message_delta" {
			t.Fatal("raw streaming fragments included in default parent output")
		}
	}
}

func TestObserveHumanKeepsParallelToolsIdentifiable(t *testing.T) {
	var out bytes.Buffer
	renderer := newAgentRenderer(&out)
	for _, event := range []events.Event{
		{"type": "run_js.started", "name": "parent", "runId": "run-A", "toolCallId": "call_11111111", "text": "// read A"},
		{"type": "run_js.started", "name": "parent", "runId": "run-A", "toolCallId": "call-22222222", "text": "// read B"},
		{"type": "stdout", "name": "parent", "runId": "run-A", "toolCallId": "call_11111111", "text": "A output\n"},
		{"type": "run_js.finished", "name": "parent", "runId": "run-A", "toolCallId": "call-22222222", "success": false, "exitCode": 7.0},
		{"type": "run_js.finished", "name": "parent", "runId": "run-A", "toolCallId": "call_11111111", "success": true, "exitCode": 0.0},
	} {
		if err := renderer.render(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := renderer.finish(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"[parent/run-A/11111111] run_js started: read A",
		"[parent/run-A/22222222] run_js started: read B",
		"[parent/run-A/22222222] run_js failed: exitCode=7",
		"[parent/run-A/11111111] run_js succeeded",
	} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("missing %q:\n%s", expected, &out)
		}
	}
	if strings.Contains(out.String(), "A output") {
		t.Fatalf("command stdout leaked into human output:\n%s", &out)
	}
}

func TestObserveHumanFormatsOnlyAcceptedAgentMarkdown(t *testing.T) {
	t.Setenv("COLUMNS", "80")
	var out bytes.Buffer
	renderer := newAgentRenderer(&out)
	answer := "**Report**\n\n| 項目 | 結果 |\n| --- | --- |\n| A | 成功 |\n| 日本語 | OK |\n\n```js\n  console.log(3);\n```"
	for _, event := range []events.Event{
		{"type": "thinking", "runId": "run", "text": "**thinking stays plain**"},
		{"type": "agent.message", "runId": "run", "text": "**Working**"},
		{"type": "agent.final", "runId": "run", "text": answer},
		{"type": "stdout", "runId": "run", "text": "**stdout stays plain**"},
		{"type": "stderr", "runId": "run", "text": "**stderr stays plain**"},
	} {
		if err := renderer.render(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := renderer.finish(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"thinking: **thinking stays plain**\n",
		"message\n  Working\n",
		"final answer\n  Report\n",
		"  | 項目   | 結果 |\n  | ------ | ---- |\n  | A      | 成功 |\n  | 日本語 | OK   |",
		"    ```js\n      console.log(3);\n    ```\n",
		"stderr: **stderr stays plain**\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, &out)
		}
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("renderer added ANSI escapes: %q", &out)
	}
	if strings.Contains(out.String(), "stdout stays plain") {
		t.Fatalf("stdout leaked into human output:\n%s", &out)
	}
}

func TestObserveHumanWrapsMarkdownToColumns(t *testing.T) {
	t.Setenv("COLUMNS", "24")
	var out bytes.Buffer
	renderer := newAgentRenderer(&out)
	if err := renderer.render(events.Event{"type": "agent.final", "runId": "run", "text": "alpha **beta gamma delta** epsilon"}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "[agent/run] final answer\n  alpha beta gamma delta\n  epsilon\n"; got != want {
		t.Fatalf("wrapped response = %q, want %q", got, want)
	}
}
