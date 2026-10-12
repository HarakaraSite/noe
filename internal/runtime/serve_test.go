package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestServeRequestsAsyncConcurrencyAndGracefulClose(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var stderr bytes.Buffer
	source := `let server;
server = noe.serve({hostname:"127.0.0.1", port:0}, request => {
  if (request.path === "/slow") {
    return (async () => { await waitForGate(); return {body: "slow"}; })();
  }
  if (request.path === "/fast") {
    const bytes = new Uint8Array([120, 102, 97, 115, 116, 121]);
    return {body: bytes.subarray(1, 5)};
  }
  if (request.path === "/head") return {body: "must not be sent"};
  if (request.path === "/empty") return {status: 204, body: "must not be sent"};
  if (request.path === "/not-modified") return {status: 304, body: "must not be sent"};
  if (request.path === "/empty-request") {
    return (async () => {
      const text = await request.text();
      const bytes = await request.bytes();
      let jsonRejected = false;
      try { await request.json(); } catch (_) { jsonRejected = true; }
      return {body: JSON.stringify({text, byteLength: bytes.length, jsonRejected})};
    })();
  }
  if (request.path === "/binary") {
    return (async () => ({body: JSON.stringify(Array.from(await request.bytes()))}))();
  }
  if (request.path === "/a b") {
    return {body: JSON.stringify({url: request.url, path: request.path})};
  }
  if (request.path === "/echo path") {
    return (async () => {
      const first = await request.bytes();
      const firstByte = first[0];
      first[0] = 0;
      first.buffer.transfer();
      const second = await request.bytes();
      const text = await request.text();
      const repeatedText = await request.text();
      const parsed = await request.json();
      const repeatedJSON = await request.json();
      const response = {
        method: request.method,
        url: request.url,
        path: request.path,
        host: request.headers.host[0],
        repeatedHeader: request.headers["x-repeat"],
        contentType: request.headers["content-type"],
        text,
        repeatedText,
        parsed,
        repeatedJSON,
        bytes: Array.from(second),
        independent: second[0] === firstByte,
      };
      server.close();
      return {
        status: 201,
        headers: {"Content-Type": "application/json", "X-Multi": ["first", "second"]},
        body: JSON.stringify(response),
      };
    })();
  }
  return {status: 404, body: "missing"};
});
if (server.hostname !== "127.0.0.1" || !Number.isInteger(server.port) || server.port === 0 ||
    server.url !== "http://" + server.hostname + ":" + server.port) throw new Error("invalid server handle");
console.log(server.url);`
	run := startServeTest(t, source, &stderr, func(h *host) error {
		return h.rt.Set("waitForGate", func() quickjs.Value {
			return h.startAsyncWork(func(ctx context.Context) (any, error) {
				select {
				case started <- struct{}{}:
				default:
				}
				select {
				case <-release:
					return nil, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			})
		})
	})
	base := run.url(t)
	client := &http.Client{Timeout: 4 * time.Second}
	ctx := run.ctx

	slowResult := make(chan struct {
		body string
		err  error
	}, 1)
	go func() {
		res, err := client.Get(base + "/slow")
		if err != nil {
			slowResult <- struct {
				body string
				err  error
			}{err: err}
			return
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		slowResult <- struct {
			body string
			err  error
		}{body: string(data), err: err}
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatalf("slow handler did not start: %v", ctx.Err())
	}

	fastResponse := doServeRequest(t, run, client, http.MethodGet, base+"/fast", nil, nil)
	if fastResponse.StatusCode != http.StatusOK || string(fastResponse.Body) != "fast" {
		t.Fatalf("fast response status=%d body=%q", fastResponse.StatusCode, fastResponse.Body)
	}
	if fastResponse.Header.Get("Content-Length") != "4" {
		t.Fatalf("typed-array view body was not fully sent: headers=%v", fastResponse.Header)
	}
	requestRaw := func(method, requestTarget string) (map[string]string, int) {
		t.Helper()
		conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", strings.TrimPrefix(base, "http://"))
		if err != nil {
			t.Fatalf("dial server for raw request: %v", err)
		}
		defer conn.Close()
		if _, err := fmt.Fprintf(conn, "%s %s HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\n\r\n", method, requestTarget); err != nil {
			t.Fatalf("write raw request: %v", err)
		}
		response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: method})
		if err != nil {
			t.Fatalf("read raw response: %v", err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read raw response body: %v", err)
		}
		var fields map[string]string
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatalf("decode raw response body %q: %v", data, err)
		}
		return fields, response.StatusCode
	}
	absolute, absoluteStatus := requestRaw(http.MethodGet, "http://example.test/a%20b?q=%2F")
	if absoluteStatus != http.StatusOK || absolute["url"] != "http://example.test/a%20b?q=%2F" || absolute["path"] != "/a b" {
		t.Fatalf("absolute-form request status=%d fields=%v", absoluteStatus, absolute)
	}
	headResponse := doServeRequest(t, run, client, http.MethodHead, base+"/head", nil, nil)
	if headResponse.StatusCode != http.StatusOK || len(headResponse.Body) != 0 {
		t.Fatalf("HEAD response status=%d body=%q", headResponse.StatusCode, headResponse.Body)
	}
	emptyResponse := doServeRequest(t, run, client, http.MethodGet, base+"/empty", nil, nil)
	if emptyResponse.StatusCode != http.StatusNoContent || len(emptyResponse.Body) != 0 {
		t.Fatalf("204 response status=%d body=%q", emptyResponse.StatusCode, emptyResponse.Body)
	}
	notModifiedResponse := doServeRequest(t, run, client, http.MethodGet, base+"/not-modified", nil, nil)
	if notModifiedResponse.StatusCode != http.StatusNotModified || len(notModifiedResponse.Body) != 0 {
		t.Fatalf("304 response status=%d body=%q", notModifiedResponse.StatusCode, notModifiedResponse.Body)
	}
	emptyRequestResponse := doServeRequest(t, run, client, http.MethodGet, base+"/empty-request", nil, nil)
	if emptyRequestResponse.StatusCode != http.StatusOK || string(emptyRequestResponse.Body) != `{"text":"","byteLength":0,"jsonRejected":true}` {
		t.Fatalf("empty request response status=%d body=%q", emptyRequestResponse.StatusCode, emptyRequestResponse.Body)
	}
	binaryRequest := doServeRequest(t, run, client, http.MethodPost, base+"/binary", nil, bytes.NewReader([]byte{0, 255, 128, 10}))
	if binaryRequest.StatusCode != http.StatusOK || string(binaryRequest.Body) != `[0,255,128,10]` {
		t.Fatalf("binary request response status=%d body=%q", binaryRequest.StatusCode, binaryRequest.Body)
	}
	close(release)
	select {
	case got := <-slowResult:
		if got.err != nil || got.body != "slow" {
			t.Fatalf("slow response body=%q err=%v", got.body, got.err)
		}
	case <-ctx.Done():
		t.Fatalf("slow handler did not finish: %v", ctx.Err())
	}

	payload := `{"message":"雪","value":42}`
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/echo%20path?q=%2F", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Add("X-Repeat", "one")
	request.Header.Add("X-Repeat", "two")
	request.Header.Add("Content-Type", "application/json")
	res, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(res.Body)
	closeErr := res.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read echo response: read=%v close=%v", readErr, closeErr)
	}
	if res.StatusCode != http.StatusCreated || !equalStrings(res.Header.Values("X-Multi"), []string{"first", "second"}) {
		t.Fatalf("echo response status=%d headers=%v", res.StatusCode, res.Header)
	}
	var echoed struct {
		Method         string         `json:"method"`
		URL            string         `json:"url"`
		Path           string         `json:"path"`
		Host           string         `json:"host"`
		RepeatedHeader []string       `json:"repeatedHeader"`
		ContentType    []string       `json:"contentType"`
		Text           string         `json:"text"`
		RepeatedText   string         `json:"repeatedText"`
		Parsed         map[string]any `json:"parsed"`
		RepeatedJSON   map[string]any `json:"repeatedJSON"`
		Bytes          []int          `json:"bytes"`
		Independent    bool           `json:"independent"`
	}
	if err := json.Unmarshal(body, &echoed); err != nil {
		t.Fatalf("decode echo response %q: %v", body, err)
	}
	parsed, _ := json.Marshal(map[string]any{"message": "雪", "value": float64(42)})
	wantBytes := make([]int, len(payload))
	for i, value := range []byte(payload) {
		wantBytes[i] = int(value)
	}
	wantURL := base + "/echo%20path?q=%2F"
	if echoed.Method != http.MethodPost || echoed.URL != wantURL || echoed.Path != "/echo path" || echoed.Host != strings.TrimPrefix(base, "http://") ||
		!equalStrings(echoed.RepeatedHeader, []string{"one", "two"}) || !equalStrings(echoed.ContentType, []string{"application/json"}) ||
		echoed.Text != payload || echoed.RepeatedText != payload || echoed.Independent != true || !equalInts(echoed.Bytes, wantBytes) {
		t.Fatalf("request conversion mismatch: %+v", echoed)
	}
	var wantJSON map[string]any
	if json.Unmarshal(parsed, &wantJSON) != nil {
		t.Fatal("failed to build expected JSON")
	}
	if left, _ := json.Marshal(echoed.Parsed); left == nil {
		t.Fatal("request.json returned an invalid value")
	} else if right, _ := json.Marshal(wantJSON); string(left) != string(right) {
		t.Fatalf("request.json=%s want=%s", left, right)
	}
	if left, _ := json.Marshal(echoed.RepeatedJSON); left == nil {
		t.Fatal("repeated request.json returned an invalid value")
	} else if right, _ := json.Marshal(wantJSON); string(left) != string(right) {
		t.Fatalf("repeated request.json=%s want=%s", left, right)
	}
	if got := run.wait(t); got.code != 0 || got.err != nil {
		t.Fatalf("run code=%d err=%v stderr=%q", got.code, got.err, stderr.String())
	}
}

func TestServeRequestURLAsteriskFormHasPath(t *testing.T) {
	request := &http.Request{
		Host: "example.test",
		URL:  &url.URL{Path: "*"},
	}
	if got, want := serveRequestURL(request), "http://example.test/*"; got != want {
		t.Fatalf("serveRequestURL=%q want=%q", got, want)
	}
}

func TestServeValidationHandlerErrorsAndRecovery(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	busyPort := busy.Addr().(*net.TCPAddr).Port
	var stderr bytes.Buffer
	source := fmt.Sprintf(`const invalid = [
  null, [], {host:"127.0.0.1"}, {hostname:""}, {hostname:1},
  {port:-1}, {port:65536}, {port:1.5}, {port:"0"}, {port:true}
];
for (const options of invalid) {
  let threw = false;
  try { noe.serve(options, () => ({})); } catch (error) { threw = true; }
  if (!threw) throw new Error("invalid options were accepted");
}
let handlerRejected = false;
try { noe.serve({}, null); } catch (error) { handlerRejected = true; }
if (!handlerRejected) throw new Error("invalid handler was accepted");
let conflictRejected = false;
try { noe.serve({port:%d}, () => ({})); } catch (error) { conflictRejected = true; }
if (!conflictRejected) throw new Error("occupied port was accepted");
let server;
server = noe.serve({}, request => {
  if (request.path === "/throw") throw new Error("sync handler exploded");
  if (request.path === "/reject") return Promise.reject(new Error("async handler rejected"));
  if (request.path === "/invalid-status") return {status:199, body:"bad"};
  if (request.path === "/invalid-header") return {headers:{"Bad Name":"bad"}, body:"bad"};
  if (request.path === "/invalid-value") return {headers:{"X-Test":"bad\r\nvalue"}, body:"bad"};
  if (request.path === "/invalid-body") return {body:new Uint16Array([1])};
  server.close();
  return {body:"recovered"};
});
console.log(server.url);`, busyPort)
	run := startServeTest(t, source, &stderr, nil)
	base := run.url(t)
	client := &http.Client{Timeout: 4 * time.Second}
	for _, path := range []string{"/throw", "/reject", "/invalid-status", "/invalid-header", "/invalid-value", "/invalid-body"} {
		response := doServeRequest(t, run, client, http.MethodGet, base+path, nil, nil)
		if response.StatusCode != http.StatusInternalServerError || string(response.Body) != "Internal Server Error\n" {
			t.Fatalf("%s response status=%d body=%q", path, response.StatusCode, response.Body)
		}
	}
	response := doServeRequest(t, run, client, http.MethodGet, base+"/ok", nil, nil)
	if response.StatusCode != http.StatusOK || string(response.Body) != "recovered" {
		t.Fatalf("recovery response status=%d body=%q", response.StatusCode, response.Body)
	}
	if got := run.wait(t); got.code != 0 || got.err != nil {
		t.Fatalf("run code=%d err=%v stderr=%q", got.code, got.err, stderr.String())
	}
	for _, want := range []string{"sync handler exploded", "async handler rejected", "response status must", "Bad Name", "response body must"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr %q does not contain %q", stderr.String(), want)
		}
	}
}

type serveTestRunResult struct {
	code int
	err  error
}

type serveTestRun struct {
	ctx      context.Context
	cancel   context.CancelFunc
	lines    <-chan string
	done     chan serveTestRunResult
	result   *serveTestRunResult
	urlValue string
	urlRead  bool
}

type serveTestLineWriter struct {
	mu      sync.Mutex
	pending []byte
	lines   chan string
}

func (w *serveTestLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, p...)
	for {
		index := bytes.IndexByte(w.pending, '\n')
		if index < 0 {
			break
		}
		line := string(w.pending[:index])
		w.pending = append(w.pending[:0], w.pending[index+1:]...)
		w.lines <- line
	}
	return len(p), nil
}

func startServeTest(t *testing.T, source string, stderr io.Writer, setup func(*host) error) *serveTestRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	lines := make(chan string, 16)
	writer := &serveTestLineWriter{lines: lines}
	done := make(chan serveTestRunResult, 1)
	ready := make(chan error, 1)
	sourceName := filepath.Join(t.TempDir(), "serve.js")
	go func() {
		h, err := newHost(Options{Stdout: writer, Stderr: stderr})
		if err == nil && setup != nil {
			err = setup(h)
		}
		if err != nil {
			if h != nil {
				_ = h.rt.Close()
			}
			ready <- err
			done <- serveTestRunResult{code: 1, err: err}
			return
		}
		ready <- nil
		code, runErr := h.run(ctx, Source{Name: sourceName, Code: source})
		_ = h.rt.Close()
		done <- serveTestRunResult{code: code, err: runErr}
	}()
	select {
	case err := <-ready:
		if err != nil {
			cancel()
			t.Fatalf("initialize serve runtime: %v", err)
		}
	case <-ctx.Done():
		cancel()
		t.Fatalf("serve runtime initialization timed out: %v", ctx.Err())
	}
	run := &serveTestRun{ctx: ctx, cancel: cancel, lines: lines, done: done}
	t.Cleanup(func() { run.cleanup(t) })
	return run
}

func (r *serveTestRun) url(t *testing.T) string {
	t.Helper()
	if r.urlRead {
		return r.urlValue
	}
	select {
	case line := <-r.lines:
		r.urlValue, r.urlRead = line, true
		return line
	case result := <-r.done:
		r.result = &result
		t.Fatalf("serve script exited before publishing its URL: code=%d err=%v", result.code, result.err)
	case <-r.ctx.Done():
		t.Fatalf("serve script did not publish its URL: %v", r.ctx.Err())
	}
	return ""
}

func (r *serveTestRun) wait(t *testing.T) serveTestRunResult {
	t.Helper()
	if r.result != nil {
		return *r.result
	}
	select {
	case result := <-r.done:
		r.result = &result
		return result
	case <-r.ctx.Done():
		t.Fatalf("serve script did not finish: %v", r.ctx.Err())
	}
	return serveTestRunResult{}
}

func (r *serveTestRun) cleanup(t *testing.T) {
	r.cancel()
	if r.result != nil {
		return
	}
	select {
	case result := <-r.done:
		r.result = &result
	case <-time.After(2 * time.Second):
		t.Error("serve runtime did not stop during cleanup")
	}
}

type serveHTTPResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func doServeRequest(t *testing.T, run *serveTestRun, client *http.Client, method, target string, headers http.Header, body io.Reader) serveHTTPResult {
	t.Helper()
	request, err := http.NewRequestWithContext(run.ctx, method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header = headers.Clone()
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, target, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response %s: %v", target, err)
	}
	return serveHTTPResult{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: data}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
