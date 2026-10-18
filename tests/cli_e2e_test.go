//go:build e2e

package e2e_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunTransformsJSONWithEnvironmentArgumentsAndNestedImports(t *testing.T) {
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "data", "input.json"), `{"name":"Ada","count":3}`)
	writeFixture(t, filepath.Join(work, "scripts", "lib", "nested", "label.js"), `export const label = await Promise.resolve("from nested module");`)
	writeFixture(t, filepath.Join(work, "scripts", "lib", "decorate.js"), `import { label } from "./nested/label.js"; export const decorate = value => ({ ...value, label, processed: true });`)
	script := filepath.Join(work, "scripts", "main.js")
	writeFixture(t, script, `import { decorate } from "./lib/decorate.js";
import { readFile, writeFile } from "node:fs/promises";
const args = process.argv.slice(2);
const [input, output] = args;
const value = JSON.parse(await readFile(input, "utf8"));
await writeFile(output, JSON.stringify(decorate(value)));
console.log(JSON.stringify({ args, cwd: process.cwd(), user: process.env.NOE_E2E_USER, missingIsUndefined: process.env.NOE_E2E_MISSING === undefined }));
console.warn("warning output");
console.error("error output");
`)

	got := runCLI(t, work, map[string]string{"NOE_E2E_USER": "e2e-user"}, "run", script, "data/input.json", "data/output.json")
	requireExit(t, got, 0)
	wantLog := fmt.Sprintf("{\"args\":[\"data/input.json\",\"data/output.json\"],\"cwd\":%q,\"user\":\"e2e-user\",\"missingIsUndefined\":true}\n", work)
	if got.stdout != wantLog {
		t.Fatalf("stdout = %q, want %q", got.stdout, wantLog)
	}
	if got.stderr != "warning output\nerror output\n" {
		t.Fatalf("stderr = %q", got.stderr)
	}
	result, err := os.ReadFile(filepath.Join(work, "data", "output.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"name":"Ada","count":3,"label":"from nested module","processed":true}` {
		t.Fatalf("output file = %q", result)
	}
}

func TestEvalAndCLICommands(t *testing.T) {
	work := t.TempDir()

	eval := runCLI(t, work, nil, "eval", `console.log(JSON.stringify(process.argv.slice(2))); console.warn("warning"); console.error("error");`)
	requireExit(t, eval, 0)
	if eval.stdout != "[]\n" || eval.stderr != "warning\nerror\n" {
		t.Fatalf("eval streams: stdout=%q stderr=%q", eval.stdout, eval.stderr)
	}

	for _, tc := range []struct {
		name       string
		args       []string
		code       int
		stdoutPart string
		stderrPart string
	}{
		{name: "help", args: []string{"--help"}, code: 0, stdoutPart: "Usage:"},
		{name: "version", args: []string{"--version"}, code: 0, stdoutPart: "noe v1.6.0\n"},
		{name: "no command prints help", args: nil, code: 0, stdoutPart: "Usage:"},
		{name: "run needs a script", args: []string{"run"}, code: 2, stderrPart: "Usage:"},
		{name: "eval needs source", args: []string{"eval"}, code: 2, stderrPart: "Usage:"},
		{name: "unknown command", args: []string{"missing"}, code: 2, stderrPart: "unknown command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runCLI(t, work, nil, tc.args...)
			requireExit(t, got, tc.code)
			if tc.stdoutPart != "" && !strings.Contains(got.stdout, tc.stdoutPart) {
				t.Errorf("stdout %q does not contain %q", got.stdout, tc.stdoutPart)
			}
			if tc.stderrPart != "" && !strings.Contains(got.stderr, tc.stderrPart) {
				t.Errorf("stderr %q does not contain %q", got.stderr, tc.stderrPart)
			}
		})
	}
}

func TestRunRejectsTypeScript(t *testing.T) {
	work := t.TempDir()
	script := filepath.Join(work, "script.ts")
	writeFixture(t, script, `console.log("must not execute");`)
	got := runCLI(t, work, nil, "run", script)
	requireExit(t, got, 1)
	if got.stdout != "" || !strings.Contains(got.stderr, "only .js and .mjs") {
		t.Fatalf("TypeScript result: stdout=%q stderr=%q", got.stdout, got.stderr)
	}
}

func TestReadmeExamples(t *testing.T) {
	work := t.TempDir()

	hello := runCLI(t, repoRootPath, nil, "run", filepath.Join("examples", "hello.js"))
	requireExit(t, hello, 0)
	if hello.stdout != "Hello from NOE!\n" || hello.stderr != "" {
		t.Fatalf("hello example: stdout=%q stderr=%q", hello.stdout, hello.stderr)
	}

	input := filepath.Join(work, "input.json")
	transformOutput := filepath.Join(work, "transform-output.json")
	writeFixture(t, input, `{"value":21}`)
	transform := runCLI(t, repoRootPath, nil, "run", filepath.Join("examples", "transform-json.js"), input, transformOutput)
	requireExit(t, transform, 0)
	if transform.stdout != "Saved "+transformOutput+"\n" || transform.stderr != "" {
		t.Fatalf("transform example: stdout=%q stderr=%q", transform.stdout, transform.stderr)
	}
	transformed, err := os.ReadFile(transformOutput)
	if err != nil {
		t.Fatal(err)
	}
	if string(transformed) != "{\n  \"value\": 21,\n  \"processed\": true\n}\n" {
		t.Fatalf("transform output = %q", transformed)
	}

	methodSeen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodSeen <- r.Method
		if r.URL.Path != "/api/data.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"source":"local fixture","count":2}`)
	}))
	defer server.Close()
	fetchOutput := filepath.Join(work, "fetch-output.json")
	fetched := runCLI(t, repoRootPath, nil, "run", filepath.Join("examples", "fetch-json.js"), server.URL+"/api/data.json", fetchOutput)
	requireExit(t, fetched, 0)
	if fetched.stdout != "Saved "+fetchOutput+"\n" || fetched.stderr != "" {
		t.Fatalf("fetch example: stdout=%q stderr=%q", fetched.stdout, fetched.stderr)
	}
	if requestMethod := <-methodSeen; requestMethod != http.MethodGet {
		t.Errorf("fetch example used method %q, want GET", requestMethod)
	}
	fetchedJSON, err := os.ReadFile(fetchOutput)
	if err != nil {
		t.Fatal(err)
	}
	if string(fetchedJSON) != "{\n  \"source\": \"local fixture\",\n  \"count\": 2\n}\n" {
		t.Fatalf("fetch output = %q", fetchedJSON)
	}
}

func TestHTTPGetPostResponsesAndErrors(t *testing.T) {
	var mu sync.Mutex
	var textMethod, jsonMethod, postMethod, postContentType, postCheck, postBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/text":
			mu.Lock()
			textMethod = r.Method
			mu.Unlock()
			fmt.Fprint(w, "hello from server")
		case "/json":
			mu.Lock()
			jsonMethod = r.Method
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"kind":"sample","count":3}`)
		case "/submit":
			post, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			mu.Lock()
			postMethod = r.Method
			postContentType = r.Header.Get("Content-Type")
			postCheck = r.Header.Get("X-E2E-Check")
			postBody = string(post)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"received":{"answer":42}}`)
		case "/bad-json":
			fmt.Fprint(w, `{"broken":`)
		case "/unavailable":
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "temporarily unavailable")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	work := t.TempDir()
	script := filepath.Join(work, "http.js")
	writeFixture(t, script, `import { writeFile } from "node:fs/promises";
const base = process.env.NOE_E2E_URL;
const textResponse = await fetch(base + "/text");
const firstText = await textResponse.text();
let secondTextReadRejected = false;
try { await textResponse.text(); } catch (_) { secondTextReadRejected = true; }
const jsonResponse = await fetch(base + "/json");
const jsonValue = await jsonResponse.json();
let secondJSONReadRejected = false;
try { await jsonResponse.json(); } catch (_) { secondJSONReadRejected = true; }
const posted = await fetch(base + "/submit", { method: "POST", headers: { "Content-Type": "application/json", "X-E2E-Check": "present" }, body: JSON.stringify({ answer: 42 }) });
const postValue = await posted.json();
const badJsonResponse = await fetch(base + "/bad-json");
let badJsonRejected = false;
try { await badJsonResponse.json(); } catch (_) { badJsonRejected = true; }
const unavailable = await fetch(base + "/unavailable");
const result = {
  textStatus: textResponse.status,
  textOK: textResponse.ok,
  firstText,
  secondTextReadRejected,
  jsonValue,
  secondJSONReadRejected,
  postStatus: posted.status,
  postOK: posted.ok,
  postValue,
  badJsonRejected,
  unavailableStatus: unavailable.status,
  unavailableOK: unavailable.ok,
  unavailableText: await unavailable.text()
};
await writeFile("http-result.json", JSON.stringify(result));
console.log("HTTP complete");
`)
	got := runCLI(t, work, map[string]string{"NOE_E2E_URL": server.URL}, "run", script)
	requireExit(t, got, 0)
	if got.stdout != "HTTP complete\n" || got.stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", got.stdout, got.stderr)
	}
	data, err := os.ReadFile(filepath.Join(work, "http-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"textStatus":200,"textOK":true,"firstText":"hello from server","secondTextReadRejected":true,"jsonValue":{"kind":"sample","count":3},"secondJSONReadRejected":true,"postStatus":201,"postOK":true,"postValue":{"received":{"answer":42}},"badJsonRejected":true,"unavailableStatus":503,"unavailableOK":false,"unavailableText":"temporarily unavailable"}`
	if string(data) != want {
		t.Fatalf("HTTP result = %s, want %s", data, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if textMethod != http.MethodGet || jsonMethod != http.MethodGet {
		t.Fatalf("default fetch methods: text=%q json=%q, want GET", textMethod, jsonMethod)
	}
	if postMethod != http.MethodPost || postContentType != "application/json" || postCheck != "present" || postBody != `{"answer":42}` {
		t.Fatalf("POST observed method=%q content-type=%q check=%q body=%q", postMethod, postContentType, postCheck, postBody)
	}
}

func TestConnectionFailureRejectsFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	work := t.TempDir()
	script := filepath.Join(work, "connection.js")
	writeFixture(t, script, `let rejected = false;
try { await fetch(process.env.NOE_E2E_URL); } catch (_) { rejected = true; }
if (!rejected) throw new Error("connection failure did not reject");
console.log("connection rejected");
`)
	got := runCLI(t, work, map[string]string{"NOE_E2E_URL": url}, "run", script)
	requireExit(t, got, 0)
	if got.stdout != "connection rejected\n" || got.stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", got.stdout, got.stderr)
	}
}

func TestStartedIOCompletesBeforeExit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, "late response")
	}))
	defer server.Close()
	work := t.TempDir()
	script := filepath.Join(work, "pending.js")
	writeFixture(t, script, `import { writeFile } from "node:fs/promises";
writeFile("file-result.txt", "file complete");
fetch(process.env.NOE_E2E_URL).then(response => response.text()).then(text => writeFile("http-result.txt", text));
console.log("script returned");
`)
	got := runCLI(t, work, map[string]string{"NOE_E2E_URL": server.URL}, "run", script)
	requireExit(t, got, 0)
	if got.stdout != "script returned\n" || got.stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", got.stdout, got.stderr)
	}
	for name, want := range map[string]string{"file-result.txt": "file complete", "http-result.txt": "late response"} {
		data, err := os.ReadFile(filepath.Join(work, name))
		if err != nil {
			t.Fatalf("read %s after CLI exit: %v", name, err)
		}
		if string(data) != want {
			t.Errorf("%s = %q, want %q", name, data, want)
		}
	}
}

func TestFileReadAndWriteFailuresReject(t *testing.T) {
	for _, tc := range []struct {
		name, source, marker string
	}{
		{name: "read", source: `import { readFile } from "node:fs/promises"; await readFile("missing-input.txt", "utf8");`, marker: "missing-input.txt"},
		{name: "write", source: `import { writeFile } from "node:fs/promises"; await writeFile("missing-directory/output.txt", "x");`, marker: "missing-directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := t.TempDir()
			script := filepath.Join(work, "failure.js")
			writeFixture(t, script, tc.source)
			got := runCLI(t, work, nil, "run", script)
			requireExit(t, got, 1)
			if got.stderr == "" || !strings.Contains(got.stderr, tc.marker) {
				t.Fatalf("stderr = %q, want diagnostic containing %q", got.stderr, tc.marker)
			}
		})
	}
}

func TestExecutionDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, source, marker string
	}{
		{name: "syntax error", source: `const broken = ;`, marker: "SyntaxError"},
		{name: "uncaught exception", source: `throw new Error("uncaught marker");`, marker: "uncaught marker"},
		{name: "late unhandled rejection", source: `Promise.resolve().then(() => { throw new Error("late rejection marker"); });`, marker: "late rejection marker"},
		{name: "never settling top-level await", source: `await new Promise(() => {});`, marker: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := t.TempDir()
			script := filepath.Join(work, "failure.js")
			writeFixture(t, script, tc.source)
			got := runCLI(t, work, nil, "run", script)
			requireExit(t, got, 1)
			if !strings.Contains(got.stderr, script) {
				t.Errorf("diagnostic does not name source file %q: %q", script, got.stderr)
			}
			if tc.marker != "" && !strings.Contains(got.stderr, tc.marker) {
				t.Errorf("diagnostic %q does not contain %q", got.stderr, tc.marker)
			}
			if got.stderr == "" {
				t.Error("failed execution produced no diagnostic")
			}
		})
	}
}

func TestExplicitExitStopsTopLevelAndAsyncContinuation(t *testing.T) {
	for _, tc := range []struct {
		name, source, wantStdout string
		code                     int
	}{
		{name: "top-level success", source: `process.exit(0); console.log("after exit");`, code: 0, wantStdout: ""},
		{name: "top-level nonzero", source: `process.exit(7); console.log("after exit");`, code: 7, wantStdout: ""},
		{name: "continuation success", source: `async function finish() { await Promise.resolve(); process.exit(0); console.log("after exit"); } finish(); console.log("started");`, code: 0, wantStdout: "started\n"},
		{name: "continuation nonzero", source: `async function finish() { await Promise.resolve(); process.exit(9); console.log("after exit"); } finish(); console.log("started");`, code: 9, wantStdout: "started\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := t.TempDir()
			script := filepath.Join(work, "exit.js")
			writeFixture(t, script, tc.source)
			got := runCLI(t, work, nil, "run", script)
			requireExit(t, got, tc.code)
			if got.stdout != tc.wantStdout {
				t.Errorf("stdout = %q, want %q", got.stdout, tc.wantStdout)
			}
			if got.stderr != "" {
				t.Errorf("unexpected stderr: %q", got.stderr)
			}
			if strings.Contains(got.stdout, "after exit") || strings.Contains(got.stderr, "after exit") {
				t.Errorf("JavaScript continued after process.exit: stdout=%q stderr=%q", got.stdout, got.stderr)
			}
		})
	}
}
