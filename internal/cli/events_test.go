package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noe/internal/events"
)

func readEvents(t *testing.T, text string) []events.Event {
	t.Helper()
	var records []events.Event
	decoder := json.NewDecoder(strings.NewReader(text))
	for {
		var event events.Event
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("invalid JSONL: %v\n%s", err, text)
		}
		if event["version"] != float64(1) || event["executionId"] == nil || event["seq"] == nil || event["time"] == nil || event["type"] == nil {
			t.Fatalf("missing envelope: %+v", event)
		}
		records = append(records, event)
	}
	return records
}

func eventTexts(records []events.Event, kind string) string {
	var text strings.Builder
	for _, record := range records {
		if record["type"] == kind {
			part, _ := record["text"].(string)
			text.WriteString(part)
		}
	}
	return text.String()
}

func TestEventsRunAndEvalStdout(t *testing.T) {
	script := `console.log('日本語');console.error('warning');process.stdout.write('tail');`
	file := filepath.Join(t.TempDir(), "output.js")
	if err := os.WriteFile(file, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"eval", "--events=-", script}, {"run", "--timeout=5s", "--events=-", file}} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), args, strings.NewReader(""), &out, &diagnostic); code != 0 {
			t.Fatalf("exit %d: %s", code, &diagnostic)
		}
		if diagnostic.Len() != 0 {
			t.Fatalf("script stderr leaked: %s", &diagnostic)
		}
		records := readEvents(t, out.String())
		if eventTexts(records, "stdout") != "日本語\ntail" || eventTexts(records, "stderr") != "warning\n" {
			t.Fatalf("records: %+v", records)
		}
		if records[0]["type"] != "execution.started" || records[len(records)-1]["type"] != "execution.finished" {
			t.Fatalf("lifecycle: %+v", records)
		}
	}
}

func TestEventsFilesAppendAndAutomaticNameInStartingCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for i := 0; i < 2; i++ {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), []string{"eval", "--events=explicit.jsonl", `console.log('hello');console.error('warning');`}, strings.NewReader(""), &out, &diagnostic); code != 0 {
			t.Fatalf("exit %d: %s", code, &diagnostic)
		}
		if out.String() != "hello\n" || diagnostic.String() != "warning\n" {
			t.Fatalf("ordinary output changed: %q %q", &out, &diagnostic)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "explicit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	records := readEvents(t, string(data))
	ids := map[any]bool{}
	for _, record := range records {
		ids[record["executionId"]] = true
	}
	if len(ids) != 2 || eventTexts(records, "stdout") != "hello\nhello\n" || eventTexts(records, "stderr") != "warning\nwarning\n" {
		t.Fatalf("append records: %+v", records)
	}
	for i := 0; i < 2; i++ {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), []string{"eval", "--events", `console.log('auto');`}, strings.NewReader(""), &out, &diagnostic); code != 0 {
			t.Fatalf("exit %d: %s", code, &diagnostic)
		}
		if !strings.Contains(diagnostic.String(), filepath.Join(dir, "noe-events-")) {
			t.Fatalf("missing path: %s", &diagnostic)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "noe-events-*.jsonl"))
	if err != nil || len(files) != 2 {
		t.Fatalf("files %v err %v", files, err)
	}
}

func TestEventsREPLPreservesUIAndRecordsResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repl.jsonl")
	input := "let x=1\nx+2\nconsole.log('hello')\nthrow new Error('boom')\nx+3\n"
	var out, diagnostic bytes.Buffer
	if code := Run(context.Background(), []string{"repl", "--events=" + path}, strings.NewReader(input), &out, &diagnostic); code != 0 {
		t.Fatalf("exit %d: %s", code, &diagnostic)
	}
	if !strings.Contains(out.String(), "> 3\n") || !strings.Contains(out.String(), "> 4\n") {
		t.Fatalf("REPL output: %q", &out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	records := readEvents(t, string(data))
	if eventTexts(records, "repl.result") != "34" || eventTexts(records, "stdout") != "hello\n" || !strings.Contains(eventTexts(records, "repl.error"), "boom") {
		t.Fatalf("REPL events: %+v", records)
	}
	if strings.Contains(string(data), "\u001b") || strings.Contains(string(data), `"text":"> "`) {
		t.Fatalf("UI leaked into events: %s", data)
	}
	out.Reset()
	diagnostic.Reset()
	if code := Run(context.Background(), []string{"repl", "--events=-"}, strings.NewReader("1+2\n"), &out, &diagnostic); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if eventTexts(readEvents(t, out.String()), "repl.result") != "3" || diagnostic.String() != "> > " {
		t.Fatalf("stdout %s stderr %q", &out, &diagnostic)
	}
}

func TestEventsUsageAndOpenFailuresDoNotRunSource(t *testing.T) {
	for _, args := range [][]string{
		{"eval", "--events=", `throw new Error('ran')`},
		{"eval", "--events", "--events=-", `throw new Error('ran')`},
		{"repl", "--timeout=1s", "--events"},
		{"eval", "--events=" + filepath.Join(t.TempDir(), "absent", "file.jsonl"), `throw new Error('ran')`},
	} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), args, strings.NewReader(""), &out, &diagnostic); code == 0 || strings.Contains(diagnostic.String(), "Error: ran") {
			t.Fatalf("args %v exit %d: %s", args, code, &diagnostic)
		}
	}
}

type failedEventWriter struct{}

func (failedEventWriter) Write([]byte) (int, error) { return 0, errors.New("event output failed") }

func TestEventsWriteFailureCancelsAndReports(t *testing.T) {
	var diagnostic bytes.Buffer
	if code := Run(context.Background(), []string{"eval", "--events=-", `await new Promise(()=>{});`}, strings.NewReader(""), failedEventWriter{}, &diagnostic); code != 1 || !strings.Contains(diagnostic.String(), "write events: event output failed") {
		t.Fatalf("exit %d: %s", code, &diagnostic)
	}
}

func TestEventOutputPreservesSplitUTF8AndPartialLine(t *testing.T) {
	var out bytes.Buffer
	w := &eventOutput{events: events.New(&out, "test"), kind: "stdout"}
	text := []byte("日\n本")
	for _, b := range text {
		if _, err := w.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	w.flush()
	if got := eventTexts(readEvents(t, out.String()), "stdout"); got != "日\n本" {
		t.Fatalf("text %q", got)
	}
}

func TestEventOutputDoesNotWaitForNewline(t *testing.T) {
	var out bytes.Buffer
	w := &eventOutput{events: events.New(&out, "test"), kind: "stdout"}
	if _, err := w.Write([]byte("progress")); err != nil {
		t.Fatal(err)
	}
	if got := eventTexts(readEvents(t, out.String()), "stdout"); got != "progress" {
		t.Fatalf("not emitted immediately: %q", got)
	}
}

func TestEventOutputRetainsTextWhenTerminalMirrorFails(t *testing.T) {
	var out bytes.Buffer
	w := &eventOutput{events: events.New(&out, "test"), kind: "stdout", mirror: failedEventWriter{}}
	if _, err := w.Write([]byte("diagnostic")); err == nil {
		t.Fatal("missing mirror error")
	}
	if text := eventTexts(readEvents(t, out.String()), "stdout"); text != "diagnostic" {
		t.Fatalf("lost event output: %q", text)
	}
}

func TestDirectObserveRunEvalAndFileRecording(t *testing.T) {
	dir := t.TempDir()
	script := `console.log('hello');console.error('warning');process.stdout.write('tail');`
	file := filepath.Join(dir, "output.js")
	if err := os.WriteFile(file, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"eval", "--observe", script},
		{"run", "--observe", file, "--observe"},
		{"eval", "--observe", "--events=" + filepath.Join(dir, "record.jsonl"), script},
	} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), args, strings.NewReader(""), &out, &diagnostic); code != 0 {
			t.Fatalf("exit %d: %s", code, &diagnostic)
		}
		if diagnostic.Len() != 0 || strings.Contains(out.String(), "hello") || !strings.Contains(out.String(), "stderr: warning") || strings.Contains(out.String(), "tail") {
			t.Fatalf("stdout=%q stderr=%q", &out, &diagnostic)
		}
		if strings.Contains(out.String(), `"executionId":`) {
			t.Fatalf("JSONL leaked into human output: %s", &out)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "record.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	records := readEvents(t, string(data))
	if eventTexts(records, "stdout") != "hello\ntail" || eventTexts(records, "stderr") != "warning\n" {
		t.Fatalf("raw recording changed: %s", data)
	}
}

func TestDirectObserveREPLKeepsPromptAndAvoidsDuplicateResult(t *testing.T) {
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), []string{"repl", "--observe"}, strings.NewReader("1+2\nconsole.log('hello')\n"), &out, &diagnostic)
	if code != 0 || diagnostic.Len() != 0 {
		t.Fatalf("exit %d stderr=%s", code, &diagnostic)
	}
	if strings.Count(out.String(), "result: 3") != 1 || strings.Contains(out.String(), "hello") || !strings.HasPrefix(out.String(), "> ") {
		t.Fatalf("REPL stdout: %q", &out)
	}
}

func TestDirectObserveConflictsAndOutputFailure(t *testing.T) {
	for _, args := range [][]string{
		{"eval", "--observe", "--events=-", `console.log("ran")`},
		{"eval", "--events=-", "--observe", `console.log("ran")`},
	} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), args, strings.NewReader(""), &out, &diagnostic); code != 2 || !strings.Contains(diagnostic.String(), "both use stdout") {
			t.Fatalf("exit %d stderr=%s", code, &diagnostic)
		}
	}
	var diagnostic bytes.Buffer
	if code := Run(context.Background(), []string{"eval", "--observe", `console.error("trigger");await new Promise(()=>{});`}, strings.NewReader(""), failedEventWriter{}, &diagnostic); code != 1 || !strings.Contains(diagnostic.String(), "write events: event output failed") {
		t.Fatalf("exit %d stderr=%s", code, &diagnostic)
	}
}
