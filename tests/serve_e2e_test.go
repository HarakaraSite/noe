//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var serveURLPattern = regexp.MustCompile(`(?m)^SERVER_URL (http://[^\s]+)\r?$`)

func waitServeURL(t *testing.T, p *openStdinCLI) string {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		if match := serveURLPattern.FindStringSubmatch(p.stdout.String()); match != nil {
			return match[1]
		}
		select {
		case <-p.stdout.changed:
		case <-p.done:
			if match := serveURLPattern.FindStringSubmatch(p.stdout.String()); match != nil {
				return match[1]
			}
			t.Fatalf("server exited before ready: out=%q err=%q", p.stdout.String(), p.stderr.String())
		case <-deadline.C:
			t.Fatalf("server not ready: out=%q err=%q", p.stdout.String(), p.stderr.String())
		}
	}
}

type serveHTTPResult struct {
	status int
	body   []byte
	err    error
}

func callServeHTTP(method, address string, body []byte) serveHTTPResult {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return serveHTTPResult{err: err}
	}
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return serveHTTPResult{err: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return serveHTTPResult{resp.StatusCode, data, err}
}

func requireServeHTTP(t *testing.T, method, address string, body []byte, status int, want []byte) {
	t.Helper()
	got := callServeHTTP(method, address, body)
	if got.err != nil || got.status != status || !bytes.Equal(got.body, want) {
		t.Fatalf("%s %s: status=%d body=%q err=%v", method, address, got.status, got.body, got.err)
	}
}

func requireServeRebind(t *testing.T, address string) {
	t.Helper()
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", u.Host)
	if err != nil {
		t.Fatalf("server did not release %s: %v", u.Host, err)
	}
	l.Close()
}

func TestServeExampleThroughCLI(t *testing.T) {
	work := t.TempDir()
	// Keep readiness framing in the test script; the example prints its URL.
	script := filepath.Join(work, "example.js")
	writeFixture(t, script, `console.log = ((log) => (...args) => log("SERVER_URL", ...args))(console.log);
await import(`+strconv.Quote("./http-server.js")+`);`)
	data, err := os.ReadFile(filepath.Join(repoRootPath, "examples", "http", "http-server.js"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(work, "http-server.js"), string(data))
	p := startCLIWithOpenStdin(t, work, "run", script, "0")
	address := waitServeURL(t, p)
	requireServeHTTP(t, "GET", address+"/health?check=1", nil, 200, []byte("ok\n"))
	requireServeHTTP(t, "POST", address+"/echo", []byte(`{"message":"日本語","numbers":[1,2]}`), 200, []byte(`{"message":"日本語","numbers":[1,2]}`))
	requireServeHTTP(t, "POST", address+"/echo", []byte("broken"), 400, []byte("Invalid JSON\n"))
	requireServeHTTP(t, "GET", address+"/missing", nil, 404, []byte("Not Found\n"))
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	requireExit(t, p.wait(t, 3*time.Second), 1)
	requireServeRebind(t, address)
	for _, args := range [][]string{{"-1"}, {"65536"}, {"x"}, {"1", "2"}} {
		got := runCLI(t, work, nil, append([]string{"run", filepath.Join(repoRootPath, "examples", "http", "http-server.js")}, args...)...)
		requireExit(t, got, 2)
		if !strings.Contains(got.stderr, "Usage:") {
			t.Fatalf("usage: %+v", got)
		}
	}
}

func TestServeAsyncIOConcurrencyBinaryAndCloseThroughCLI(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var releaseOnce sync.Once
	releaseUpstream := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-release:
			io.WriteString(w, "slow finished")
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { releaseUpstream(); upstream.Close() })
	work := t.TempDir()
	payload := bytes.Repeat(binaryPayload(), 2048)
	if err := os.WriteFile(filepath.Join(work, "payload.bin"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(work, "server.js")
	writeFixture(t, script, `import {readFile} from "node:fs/promises";
const server = serve({}, async request => {
  const path = new URL(request.url).pathname;
  if(path === "/slow") return new Response(await (await fetch(`+strconv.Quote(upstream.URL)+`)).text());
  if(path === "/binary") return new Response(await readFile("payload.bin"));
  if(path === "/bad") throw new Error("HANDLER_FAILED");
  if(path === "/stop") { setTimeout(() => { server.close(); server.close(); console.log("SERVER_CLOSED"); }, 50); return new Response(await readFile("payload.bin")); }
  return new Response("fast");
}); console.log("SERVER_URL",server.url);`)
	p := startCLIWithOpenStdin(t, work, "run", script)
	address := waitServeURL(t, p)
	slow := make(chan serveHTTPResult, 1)
	go func() { slow <- callServeHTTP("GET", address+"/slow", nil) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("async handler did not start fetch")
	}
	requireServeHTTP(t, "GET", address+"/fast", nil, 200, []byte("fast"))
	requireServeHTTP(t, "GET", address+"/binary", nil, 200, payload)
	requireServeHTTP(t, "GET", address+"/bad", nil, 500, []byte("Internal Server Error"))
	requireServeHTTP(t, "GET", address+"/fast", nil, 200, []byte("fast"))
	// Let the slow handler finish before closing: the stdlib close call waits
	// for accepted requests to complete.
	releaseUpstream()
	select {
	case got := <-slow:
		if got.err != nil || string(got.body) != "slow finished" {
			t.Fatalf("slow=%+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("slow handler did not finish")
	}
	requireServeHTTP(t, "GET", address+"/stop", nil, 200, payload)
	if !p.stdout.waitFor("SERVER_CLOSED\n", 3*time.Second) {
		t.Fatalf("server close did not finish: out=%q err=%q", p.stdout.String(), p.stderr.String())
	}
	requireServeRebind(t, address)
	got := p.wait(t, 3*time.Second)
	requireExit(t, got, 0)
	if !strings.Contains(got.stderr, "HANDLER_FAILED") {
		t.Fatalf("handler diagnostic: %+v", got)
	}
	requireServeRebind(t, address)
}

func TestServeExecutionEndingsThroughCLI(t *testing.T) {
	for _, tc := range []struct {
		name, ending string
		code         int
		diagnostic   string
	}{
		{"idle timeout", "", 124, "execution timed out"},
		{"main failure", `; throw new Error("MAIN_FAILED")`, 1, "MAIN_FAILED"},
		{"rejection while awaiting", `; Promise.reject(new Error("BACKGROUND_FAILED")); await new Promise(()=>{})`, 1, "BACKGROUND_FAILED"},
		{"rejection with pending timer", `; setTimeout(()=>{},60000); Promise.reject(new Error("BACKGROUND_FAILED")); await new Promise(()=>{})`, 1, "BACKGROUND_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := startCLIWithOpenStdin(t, t.TempDir(), "eval", "--timeout=1s", `const s=serve({},()=>new Response("ok")); console.log("SERVER_URL",s.url)`+tc.ending)
			address := waitServeURL(t, p)
			got := p.wait(t, 3*time.Second)
			requireExit(t, got, tc.code)
			if !strings.Contains(got.stderr, tc.diagnostic) {
				t.Fatalf("ending: %+v", got)
			}
			requireServeRebind(t, address)
		})
	}
	t.Run("handler JS timeout", func(t *testing.T) {
		p := startCLIWithOpenStdin(t, t.TempDir(), "eval", "--timeout=1s", `const s=serve({},()=>{console.log("HANDLER_LOOP");while(true){} }); console.log("SERVER_URL",s.url)`)
		address := waitServeURL(t, p)
		response := make(chan serveHTTPResult, 1)
		go func() { response <- callServeHTTP("GET", address, nil) }()
		if !p.stdout.waitFor("HANDLER_LOOP\n", 3*time.Second) {
			t.Fatal("handler did not run")
		}
		got := p.wait(t, 3*time.Second)
		requireExit(t, got, 124)
		select {
		case <-response:
		case <-time.After(3 * time.Second):
			t.Fatal("HTTP client not released")
		}
		requireServeRebind(t, address)
	})
	t.Run("explicit exit", func(t *testing.T) {
		p := startCLIWithOpenStdin(t, t.TempDir(), "eval", `const s=serve({},()=>{try{process.exit(7)}finally{console.log("AFTER_EXIT")} }); console.log("SERVER_URL",s.url)`)
		address := waitServeURL(t, p)
		callServeHTTP("GET", address, nil)
		got := p.wait(t, 3*time.Second)
		requireExit(t, got, 7)
		if strings.Contains(got.stdout, "AFTER_EXIT") || got.stderr != "" {
			t.Fatalf("exit=%+v", got)
		}
		requireServeRebind(t, address)
	})
}

func TestServeMultipleListenersCloseIndependentlyThroughCLI(t *testing.T) {
	p := startCLIWithOpenStdin(t, t.TempDir(), "eval", `const first=serve({},()=>{setTimeout(()=>{first.close();console.log("FIRST_CLOSED")},50);return new Response("first closed")});
const second=serve({},request=>{if(new URL(request.url).pathname==="/stop")setTimeout(()=>{second.close();console.log("SECOND_CLOSED")},50);return new Response("second alive")});
console.log("SERVER_URL",first.url);console.log("SECOND_URL",second.url);`)
	first := waitServeURL(t, p)
	if !p.stdout.waitFor("SECOND_URL http://", 3*time.Second) {
		t.Fatal("second listener did not start")
	}
	match := regexp.MustCompile(`(?m)^SECOND_URL (http://[^\s]+)$`).FindStringSubmatch(p.stdout.String())
	if match == nil {
		t.Fatalf("second listener URL missing: %q", p.stdout.String())
	}
	second := match[1]
	requireServeHTTP(t, "GET", first, nil, 200, []byte("first closed"))
	if !p.stdout.waitFor("FIRST_CLOSED\n", 3*time.Second) {
		t.Fatalf("first server close did not finish: out=%q err=%q", p.stdout.String(), p.stderr.String())
	}
	requireServeRebind(t, first)
	requireServeHTTP(t, "GET", second, nil, 200, []byte("second alive"))
	requireServeHTTP(t, "GET", second+"/stop", nil, 200, []byte("second alive"))
	if !p.stdout.waitFor("SECOND_CLOSED\n", 3*time.Second) {
		t.Fatalf("second server close did not finish: out=%q err=%q", p.stdout.String(), p.stderr.String())
	}
	got := p.wait(t, 3*time.Second)
	requireExit(t, got, 0)
	if got.stderr != "" {
		t.Fatalf("multiple listeners: %+v", got)
	}
	requireServeRebind(t, second)
}

func TestServeImmediateCloseIsIdempotentThroughCLI(t *testing.T) {
	got := runCLI(t, t.TempDir(), nil, "eval", `const first=serve({},()=>new Response()); first.close(); first.close(); console.log("CLOSED");`)
	requireExit(t, got, 0)
	if got.stdout != "CLOSED\n" || got.stderr != "" {
		t.Fatalf("immediate close/rebind: %+v", got)
	}
}

func TestServeDisconnectedClientDoesNotStopOtherHandlers(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseUpstream := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
			io.WriteString(w, "finished")
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { releaseUpstream(); upstream.Close() })
	p := startCLIWithOpenStdin(t, t.TempDir(), "eval", `const s=serve({},async request=>{
const path=new URL(request.url).pathname;
if(path==="/slow"){await fetch(`+strconv.Quote(upstream.URL)+`);console.log("LATE_HANDLER_FINISHED");return new Response("late")}
	if(path==="/stop")setTimeout(()=>{s.close();console.log("SERVER_CLOSED")},50);return new Response("ok");});console.log("SERVER_URL",s.url);`)
	address := waitServeURL(t, p)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", address+"/slow", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	defer client.CloseIdleConnections()
	requestDone := make(chan error, 1)
	go func() {
		resp, err := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("slow request did not start")
	}
	cancel()
	select {
	case err := <-requestDone:
		if err == nil {
			t.Fatal("cancelled HTTP request succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("disconnected client did not return")
	}
	requireServeHTTP(t, "GET", address, nil, 200, []byte("ok"))
	releaseUpstream()
	if !p.stdout.waitFor("LATE_HANDLER_FINISHED\n", 3*time.Second) {
		t.Fatal("handler did not finish after disconnect")
	}
	requireServeHTTP(t, "GET", address+"/stop", nil, 200, []byte("ok"))
	if !p.stdout.waitFor("SERVER_CLOSED\n", 3*time.Second) {
		t.Fatalf("server close did not finish: out=%q err=%q", p.stdout.String(), p.stderr.String())
	}
	got := p.wait(t, 3*time.Second)
	requireExit(t, got, 0)
	if got.stderr != "" {
		t.Fatalf("disconnect affected server: %+v", got)
	}
	requireServeRebind(t, address)
}
