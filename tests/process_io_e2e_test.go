//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessIOOpenStdinRespondsBeforeEOF(t *testing.T) {
	source := `import {stdin,stdout} from "noe:io";
const {value,done}=await stdin.getReader().read();
if(done) throw Error("stdin unexpectedly ended");
await stdout.getWriter().write(value);
console.log("response complete");`
	for _, mode := range []string{"run", "eval"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			args := []string{"eval", source}
			if mode == "run" {
				script := filepath.Join(work, "stdio.js")
				writeFixture(t, script, source)
				args = []string{"run", script}
			}
			process := startCLIWithOpenStdin(t, work, args...)
			const request = "request-before-eof\n"
			if _, err := io.WriteString(process.stdin, request); err != nil {
				t.Fatal(err)
			}
			if !process.stdout.waitFor(request, 3*time.Second) {
				t.Fatalf("stream did not answer while stdin remained open\nstdout:\n%s\nstderr:\n%s", process.stdout.String(), process.stderr.String())
			}
			if !process.stdout.waitFor("response complete\n", time.Second) {
				t.Fatalf("script did not finish after its first input chunk\nstdout:\n%s\nstderr:\n%s", process.stdout.String(), process.stderr.String())
			}
			got := process.wait(t, 3*time.Second)
			requireExit(t, got, 0)
			if got.stdout != request+"response complete\n" || got.stderr != "" {
				t.Fatalf("open-stdin response: %+v", got)
			}
		})
	}
}

func TestProcessIOStreamPipePreservesBinaryAboveCaptureLimit(t *testing.T) {
	const size = 40 << 20
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(i*31 + 7)
	}
	source := `import {stdin,stdout,pipe} from "noe:io"; await pipe(stdin,stdout);`
	got := runCLIWithInput(t, t.TempDir(), string(payload), "eval", source)
	requireExit(t, got, 0)
	if got.stderr != "" || !bytes.Equal([]byte(got.stdout), payload) {
		t.Fatalf("stream pipe changed binary payload: stdout bytes=%d want=%d stderr=%q", len(got.stdout), len(payload), got.stderr)
	}
}

func TestProcessIONativeStreamRunAndEvalKeepNormalOutput(t *testing.T) {
	source := `import {stdout} from "noe:io";
await stdout.getWriter().write(new TextEncoder().encode("native stream output\n"));`
	for _, mode := range []string{"run", "eval"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			args := []string{"eval", source}
			if mode == "run" {
				script := filepath.Join(work, "output.js")
				writeFixture(t, script, source)
				args = []string{"run", script}
			}
			got := runCLI(t, work, nil, args...)
			requireExit(t, got, 0)
			if got.stdout != "native stream output\n" || got.stderr != "" {
				t.Fatalf("native stream output: %+v", got)
			}
		})
	}
}

func TestProcessIONativeStreamUsesEventsAndObserveOutputs(t *testing.T) {
	const marker = "native stream event output"
	source := `import {stdout} from "noe:io";
await stdout.getWriter().write(new TextEncoder().encode("` + marker + `"));`
	work := t.TempDir()

	jsonl := runCLI(t, work, nil, "eval", "--events=-", source)
	requireExit(t, jsonl, 0)
	if jsonl.stderr != "" || !strings.Contains(processIOEventText(t, jsonl.stdout, "stdout"), marker) {
		t.Fatalf("--events=- did not record native stream output: %+v", jsonl)
	}

	eventPath := filepath.Join(work, "observed.jsonl")
	scriptPath := filepath.Join(work, "observed.js")
	writeFixture(t, scriptPath, source)
	observed := runCLI(t, work, nil, "run", "--observe", "--events="+eventPath, scriptPath)
	requireExit(t, observed, 0)
	if observed.stderr != "" || strings.Contains(observed.stdout, marker) || strings.Contains(observed.stdout, `"executionId"`) {
		t.Fatalf("--observe did not retain human output routing: %+v", observed)
	}
	data, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(processIOEventText(t, string(data), "stdout"), marker) {
		t.Fatalf("--observe recording omitted native stream output: %s", data)
	}
}

func TestProcessIOBlockedStdinReadCancelsOnTimeout(t *testing.T) {
	process := startCLIWithOpenStdin(t, t.TempDir(), "eval", "--timeout=250ms", `import {stdin} from "noe:io";
console.log("WAITING_FOR_STREAM_INPUT");
await stdin.getReader().read();
console.log("READ_AFTER_TIMEOUT");`)
	if !process.stdout.waitFor("WAITING_FOR_STREAM_INPUT\n", 3*time.Second) {
		t.Fatalf("script did not reach stream read\nstdout:\n%s\nstderr:\n%s", process.stdout.String(), process.stderr.String())
	}
	got := process.wait(t, 3*time.Second)
	requireExit(t, got, 124)
	if !strings.Contains(got.stderr, "execution timed out after 250ms") || strings.Contains(got.stdout, "READ_AFTER_TIMEOUT") {
		t.Fatalf("blocked stdin timeout: %+v", got)
	}
}

func processIOEventText(t *testing.T, jsonl, kind string) string {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(jsonl))
	var text strings.Builder
	for {
		var event map[string]any
		err := decoder.Decode(&event)
		if err == io.EOF {
			return text.String()
		}
		if err != nil {
			t.Fatalf("decode process I/O events: %v\n%s", err, jsonl)
		}
		if event["type"] == kind {
			if part, ok := event["text"].(string); ok {
				text.WriteString(part)
			}
		}
	}
}
