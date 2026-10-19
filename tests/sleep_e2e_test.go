//go:build e2e

package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSleepExampleAndUnawaitedCompletionThroughCLI(t *testing.T) {
	got := runCLI(t, t.TempDir(), nil, "run", filepath.Join(repoRootPath, "examples", "basics", "sleep-demo.js"))
	requireExit(t, got, 0)
	if got.stdout != "実行 1\n実行 2\n実行 3\n" || got.stderr != "" {
		t.Fatalf("demo=%+v", got)
	}
	got = runCLI(t, t.TempDir(), nil, "eval", `import {setTimeout as sleep} from "timers/promises"; sleep(20).then(v=>console.log("FINISHED",v === undefined));console.log("STARTED");`)
	requireExit(t, got, 0)
	if got.stdout != "STARTED\nFINISHED true\n" || got.stderr != "" {
		t.Fatalf("completion=%+v", got)
	}
}

func TestSleepTimeoutAndExplicitExitThroughCLI(t *testing.T) {
	for _, awaited := range []bool{false, true} {
		source := `import {setTimeout as sleep} from "timers/promises"; console.log("STARTED");const p=sleep(60000).then(()=>console.log("AFTER"));`
		if awaited {
			source += `await p;`
		}
		requireExecutionTimeout(t, runCLI(t, t.TempDir(), nil, "eval", "--timeout=200ms", source))
	}
	got := runCLI(t, t.TempDir(), nil, "eval", `import {setTimeout as sleep} from "timers/promises"; sleep(60000).then(()=>console.log("AFTER"));process.exit(7);`)
	requireExit(t, got, 7)
	if got.stdout != "" || got.stderr != "" {
		t.Fatalf("explicit exit=%+v", got)
	}
}

func TestSleepDoesNotBlockServeHandlersThroughCLI(t *testing.T) {
	p := startCLIWithOpenStdin(t, t.TempDir(), "eval", `import {setTimeout as sleep} from "timers/promises"; const s=serve({},async request=>{
const path=new URL(request.url).pathname;
if(path==="/slow"){const p=sleep(60000);console.log("SLEEP_HANDLER_STARTED");await p;console.log("SLEEP_HANDLER_FINISHED");return new Response("late")}
if(path==="/exit")process.exit(0);return new Response("fast");});console.log("SERVER_URL",s.url);`)
	address := waitServeURL(t, p)
	slow := make(chan serveHTTPResult, 1)
	go func() { slow <- callServeHTTP("GET", address+"/slow", nil) }()
	if !p.stdout.waitFor("SLEEP_HANDLER_STARTED\n", 3*time.Second) {
		t.Fatal("sleeping handler did not start")
	}
	requireServeHTTP(t, "GET", address+"/fast", nil, 200, []byte("fast"))
	callServeHTTP("GET", address+"/exit", nil)
	got := p.wait(t, 3*time.Second)
	requireExit(t, got, 0)
	if got.stderr != "" || strings.Contains(got.stdout, "SLEEP_HANDLER_FINISHED") {
		t.Fatalf("server timer=%+v", got)
	}
	select {
	case got := <-slow:
		// On forced shutdown net/http may flush its default empty response
		// before the connection closes. The cancelled JS must not reply.
		if got.err == nil && len(got.body) != 0 {
			t.Fatalf("cancelled handler returned a body: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sleeping request was not released")
	}
	requireServeRebind(t, address)
}
