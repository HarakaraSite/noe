package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
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
		if !strings.Contains(diagnostic.String(), filepath.Join(dir, ".noe", "events", "noe-events-")) {
			t.Fatalf("missing path: %s", &diagnostic)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, ".noe", "events", "noe-events-*.jsonl"))
	if err != nil || len(files) != 2 {
		t.Fatalf("files %v err %v", files, err)
	}
}

func TestEventsUsageAndOpenFailuresDoNotRunSource(t *testing.T) {
	for _, args := range [][]string{
		{"eval", "--events=", `throw new Error('ran')`},
		{"eval", "--events", "--events=-", `throw new Error('ran')`},
		{"eval", "--events=" + filepath.Join(t.TempDir(), "absent", "file.jsonl"), `throw new Error('ran')`},
	} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), args, strings.NewReader(""), &out, &diagnostic); code == 0 || strings.Contains(diagnostic.String(), "Error: ran") {
			t.Fatalf("args %v exit %d: %s", args, code, &diagnostic)
		}
	}
}

type failedEventWriter struct{}

type abortableEventSink struct {
	bytes.Buffer
	marker string
}

type pendingRenderSink struct{ started chan struct{} }

func (*pendingRenderSink) Write(p []byte) (int, error) { return len(p), nil }
func (w *pendingRenderSink) WriteContext(ctx context.Context, p []byte) (int, error) {
	close(w.started)
	<-ctx.Done()
	return 0, ctx.Err()
}

func TestEventsCancelledDisplayPreservesCommittedRecordSequence(t *testing.T) {
	var record bytes.Buffer
	display := &pendingRenderSink{make(chan struct{})}
	out := &renderedEventsWriter{record: &record, renderer: newAgentRenderer(display)}
	w := events.New(out, "recorded")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.EmitContext(ctx, events.Event{"type": "stderr", "text": "pending"}) }()
	<-display.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("emit=%v", err)
	}
	if w.Err() != nil {
		t.Fatalf("complete recording was poisoned: %v", w.Err())
	}
	w.Emit(events.Event{"type": "stdout", "text": "continued"})
	records := readEvents(t, record.String())
	if len(records) != 2 || records[0]["seq"] != float64(1) || records[1]["seq"] != float64(2) {
		t.Fatalf("committed sequence lost: %s", record.String())
	}
}

func (w *abortableEventSink) WriteContext(ctx context.Context, p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"text":"blocked"`)) {
		if err := os.WriteFile(w.marker, []byte("started"), 0600); err != nil {
			return 0, err
		}
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return w.Buffer.Write(p)
}

func TestEventsStreamAbortWithNoRecordBytesAllowsContinuation(t *testing.T) {
	out := &abortableEventSink{marker: filepath.Join(t.TempDir(), "write-started")}
	var diagnostic bytes.Buffer
	source := `import {stdout,pipe} from "noe:io"; import {existsSync} from "node:fs";
const c=new AbortController(),reason=new Error("expected abort");
const source=new ReadableStream({start(controller){controller.enqueue(new TextEncoder().encode("blocked"));controller.close()}});
const p=pipe(source,stdout,{signal:c.signal});
const check=()=>{if(existsSync(` + strconv.Quote(out.marker) + `))c.abort(reason);else setTimeout(check,1)};check();
try{await p;throw Error("abort did not reject")}catch(e){if(e!==reason)throw e}
console.log("continued after stream abort");`
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	code := Run(ctx, []string{"eval", "--events=-", source}, strings.NewReader(""), out, &diagnostic)
	if code != 0 || diagnostic.Len() != 0 {
		t.Fatalf("code=%d diagnostic=%q output=%q", code, diagnostic.String(), out.String())
	}
	if _, err := os.Stat(out.marker); err != nil {
		t.Fatal("write was never pending", err)
	}
	records := readEvents(t, out.String())
	var continued bool
	for _, event := range records {
		if event["type"] == "stdout" && event["text"] == "continued after stream abort\n" {
			continued = true
		}
	}
	if !continued {
		t.Fatalf("no continuation event: %s", out.String())
	}
}

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
