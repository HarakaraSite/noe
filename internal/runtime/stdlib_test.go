package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func runStdlib(t *testing.T, source string, opts Options) string {
	t.Helper()
	var out bytes.Buffer
	opts.Stdout = &out
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "main.mjs"), source}, opts)
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v out=%q", code, err, &out)
	}
	return out.String()
}

func TestStdlibFilesProcessTimersAndModules(t *testing.T) {
	t.Setenv("NOE_STDLIB_TEST", "available")
	dir := t.TempDir()
	name, _ := json.Marshal(filepath.Join(dir, "data.txt"))
	cwd, _ := json.Marshal(dir)
	source := `import fs from "node:fs";
 import {readFile, writeFile} from "fs/promises";
 import path from "path";
 import os from "node:os";
 import {setTimeout as delay} from "node:timers/promises";
 import {createHash} from "node:crypto";
 import * as dns from "dns/promises";
 import * as http from "http";
 import * as vm from "vm";
 import * as zlib from "zlib";
 import * as util from "util";
 import * as events from "events";
 import * as assert from "assert";
 import * as stream from "stream/web";
 import {MessageChannel as ModuleMessageChannel, BroadcastChannel as ModuleBroadcastChannel} from "node:worker_threads";
 const file = ` + string(name) + `;
 await writeFile(file, new TextEncoder().encode("日本語"));
 if (await readFile(file, "utf8") !== "日本語" || fs.readFileSync(file, "utf8") !== "日本語") throw Error("file");
 if (!path.isAbsolute(file) || !os.homedir() || typeof dns.lookup !== "function") throw Error("modules");
 if (typeof noe !== "undefined") throw Error("legacy global present");
 if (process.env.NOE_STDLIB_TEST !== "available" || process.argv.slice(2).join() !== "one,two" || process.cwd() !== ` + string(cwd) + `) throw Error("process");
 if (MessageChannel !== ModuleMessageChannel || BroadcastChannel !== ModuleBroadcastChannel) throw Error("channel exports differ");
 const channel = new MessageChannel();
 if (channel.constructor !== MessageChannel) throw Error("channel constructor differs");
 channel.port1.close(); channel.port2.close();
 const broadcast = new BroadcastChannel("identity");
 if (broadcast.constructor !== BroadcastChannel) throw Error("broadcast constructor differs");
 broadcast.close();
 if (createHash("sha256").update("abc").digest("hex") !== "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad") throw Error("hash");
 let n = 0;
 await new Promise(resolve => { const id = setInterval(() => { if (++n === 2) { clearInterval(id); resolve(); } }, 1); });
 if (await delay(1, 42) !== 42) throw Error("timer");
 await delay(1);
 console.log("stdlib ready");`
	if out := runStdlib(t, source, Options{Cwd: dir, Args: []string{"one", "two"}}); out != "stdlib ready\n" {
		t.Fatal(out)
	}
}

func TestStdlibHTTPAndWebSocket(t *testing.T) {
	source := `const server = serve({port:0}, async request => {
  const pathname = new URL(request.url).pathname;
  if (pathname === "/socket") {
   const {socket, response} = upgradeWebSocket(request);
   socket.onmessage = e => socket.send(e.data);
   return response;
  }
  if (pathname === "/echo") {
   const body = await request.text();
   return new Response(JSON.stringify({method:request.method, body, header:request.headers.get("x-test")}),
    {status:201, headers:{"Content-Type":"application/json"}});
  }
  if (pathname === "/missing") return new Response("missing", {status:404});
  return new Response(new ReadableStream({start(c) { c.enqueue(new TextEncoder().encode("streamed")); c.close(); }}));
 });
 const response = await fetch(new Request(server.url));
 if (await response.text() !== "streamed" || !response.bodyUsed) throw Error("fetch stream");
 let consumed = false; try { await response.text(); } catch (_) { consumed = true; }
 if (!consumed) throw Error("body consumed twice");
 const echo = await fetch(server.url + "/echo", {method:"POST", headers:{"X-Test":"header"}, body:"日本語"});
 const echoed = await echo.json();
 if (echo.status !== 201 || !echo.ok || echoed.method !== "POST" || echoed.body !== "日本語" || echoed.header !== "header") throw Error("fetch request");
 const missing = await fetch(server.url + "/missing");
 if (missing.status !== 404 || missing.ok || await missing.text() !== "missing") throw Error("HTTP error response");
 const socket = new WebSocket(server.url.replace("http:", "ws:") + "/socket");
 await new Promise((resolve, reject) => {
  socket.onopen = () => socket.send("echo");
  socket.onerror = reject;
  socket.onmessage = e => { if (e.data !== "echo") reject(Error("websocket")); socket.close(); };
  socket.onclose = resolve;
 });
 server.close();
 console.log("http and websocket ready");`
	if out := runStdlib(t, source, Options{}); out != "http and websocket ready\n" {
		t.Fatal(out)
	}
}

func TestStdlibCommandHelper(t *testing.T) {
	if os.Getenv("NOE_STDLIB_COMMAND_HELPER") == "1" {
		fmt.Print("child ready")
		os.Exit(0)
	}
}

func TestStdlibCommandsAndWorkers(t *testing.T) {
	t.Setenv("NOE_STDLIB_COMMAND_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command, _ := json.Marshal(executable)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "value.mjs"), []byte(`export const value = 42;`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "worker.mjs"), []byte(`import {parentPort} from "node:worker_threads"; import {value} from "./value.mjs"; parentPort.postMessage(value);`), 0600); err != nil {
		t.Fatal(err)
	}
	source := `import {execFile, execFileSync} from "node:child_process";
 import {Worker} from "node:worker_threads";
 const command = ` + string(command) + `;
 if (execFileSync(command, ["-test.run=^TestStdlibCommandHelper$"]) !== "child ready") throw Error("sync command");
 if ((await execFile(command, ["-test.run=^TestStdlibCommandHelper$"])).stdout !== "child ready") throw Error("async command");
 const worker = new Worker("./worker.mjs");
 const value = await new Promise((resolve,reject) => { worker.on("message",resolve); worker.on("error",reject); });
 if (value !== 42) throw Error("file worker");
 const web = new Worker("./worker.mjs", {type:"module"});
 await new Promise((resolve,reject) => { web.onmessage=e=>resolve(e.data); web.onerror=reject; });
 await web.terminate();
 console.log("commands and workers ready");`
	// The second worker uses the web global rather than the imported Node class.
	source = strings.Replace(source, `const web = new Worker`, `const web = new globalThis.Worker`, 1)
	if out := runStdlib(t, source, Options{Cwd: dir}); out != "commands and workers ready\n" {
		t.Fatal(out)
	}
}

func TestStdlibRootErrorsStopContinuousWork(t *testing.T) {
	for _, source := range []string{
		`setInterval(()=>{},1); throw Error("root failure")`,
		`serve({port:0},()=>new Response("x")); throw Error("root failure")`,
		`setTimeout(()=>{throw Error("root failure")},1); await new Promise(()=>{})`,
	} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "error.js"), source}, Options{})
		cancel()
		if err == nil || !strings.Contains(err.Error(), "root failure") || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("source=%s err=%v", source, err)
		}
	}
}

func TestStdlibSessionTimerRecoveryAndBinding(t *testing.T) {
	var out bytes.Buffer
	s, err := NewSession(Options{Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	result := s.Eval(context.Background(), `var retained=42`)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	result = s.Eval(ctx, `await (setInterval(()=>console.log("stale"),1000), new Promise(()=>{}))`)
	cancel()
	if result.Exit || !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("cancel=%+v", result)
	}
	result = s.Eval(context.Background(), `retained + 1`)
	if result.Err != nil || result.Output != "43" {
		t.Fatalf("recovery=%+v", result)
	}
	result = s.Eval(context.Background(), `const binding = await (await import("node:timers/promises")).setTimeout(1).then(()=>{setTimeout(()=>console.log(binding),0); return 42;})`)
	if result.Err != nil || out.String() != "42\n" {
		t.Fatalf("binding=%+v out=%q", result, &out)
	}
	result = s.Eval(context.Background(), `setInterval(()=>{},1); throw Error("ordinary failure")`)
	if result.Exit || result.Err == nil {
		t.Fatalf("error=%+v", result)
	}
	result = s.Eval(context.Background(), `await new Promise(resolve=>setTimeout(()=>resolve(44),1))`)
	if result.Err != nil || result.Output != "44" {
		t.Fatalf("fresh timers=%+v", result)
	}
}

func TestStdlibSessionActiveIOAndUnrefWorkerCancellation(t *testing.T) {
	for _, setup := range []string{
		`serve({port:0},()=>new Response("x"));`,
		`const {Worker}=await import("node:worker_threads"); const worker=new Worker("setInterval(()=>{},1000)",{eval:true}); worker.unref();`,
		`const port=new MessageChannel(); port.port1.onmessage=()=>{}; port.port1.unref();`,
	} {
		s, err := NewSession(Options{})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		// A module permits multiple await statements within this single REPL expression.
		input := `await (async()=>{` + setup + `await new Promise(()=>{});})()`
		result := s.Eval(ctx, input)
		cancel()
		s.Close()
		if !result.Exit || !errors.Is(result.Err, quickjs.ErrClosed) {
			t.Fatalf("setup=%s result=%+v", setup, result)
		}
	}
}

func TestStdlibProcessExit(t *testing.T) {
	for _, source := range []string{`process.exit(7)`, `process.exitCode=7`, `setInterval(()=>{},1); process.exit(7)`} {
		code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "exit.js"), source}, Options{})
		if code != 7 || err != nil {
			t.Fatalf("source=%s code=%d err=%v", source, code, err)
		}
	}
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result := s.Eval(context.Background(), `setInterval(()=>{},1); process.exit(7)`)
	if !result.Exit || result.Code != 7 || result.Err != nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestStdlibSessionCompletedResourcesAllowRecovery(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.mjs"), []byte(`// complete naturally`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		`var worker = new Worker("empty.mjs",{type:"module"})`,
		`var ports = new MessageChannel(); ports.port1.close()`,
		`var bc = new BroadcastChannel("completed"); bc.close()`,
	} {
		s, err := NewSession(Options{Cwd: dir})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		first := s.Eval(ctx, source)
		if first.Err != nil {
			t.Fatal(first.Err)
		}
		failed := s.Eval(ctx, `throw Error("recoverable")`)
		recovered := s.Eval(ctx, `42`)
		cancel()
		s.Close()
		if failed.Exit || failed.Err == nil || recovered.Err != nil || recovered.Output != "42" {
			t.Fatalf("source=%s failed=%+v recovered=%+v", source, failed, recovered)
		}
	}
}
