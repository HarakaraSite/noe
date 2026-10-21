//go:build e2e && linux

package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestLowLevelAgentTaskCancelAndParentTimeout(t *testing.T) {
	for _, stop := range []string{"cancel-task", "timeout", "SIGINT"} {
		t.Run(stop, func(t *testing.T) {
			work := lowLevelFixture(t)
			readyFile := filepath.Join(work, "run-js-ready")
			var calls atomic.Int32
			server := agentAPIFixture(t, func(_ context.Context, _ agentChatRequest) agentModelReply {
				if calls.Add(1) != 1 {
					t.Error("canceled run_js unexpectedly requested another model response")
					return agentModelReply{Content: "unexpected"}
				}
				code := `// Wait on owned resources until the parent cancels this run.
import {open} from "noe:fs-handle";import {listen} from "noe:sockets";import {subscribe} from "noe:signals";
import {writeFileSync} from "node:fs";
await open("index.bin",{mode:"r+"});subscribe("SIGTERM");
const listener=await listen({network:"unix",path:"./agent.sock"});`
				code += `writeFileSync("run-js-ready","ready");await listener.accept();`
				return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("owned-low-level", code)}}}, FinishReason: "tool_calls"}
			})
			defer server.Close()
			source := `import {spawn_agent,cancel_task,wait_task} from "noe:agent";
import {readFile} from "node:fs/promises";
const a=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"wait on low-level resources");
const commandId=a.commandId;
const pause=()=>new Promise(r=>setTimeout(r,2));
let ready=false;for(let n=0;n<2000;n++){try{await readFile("run-js-ready");ready=true;break}catch{await pause()}}
if(!ready)throw Error("run_js did not become ready");`
			if stop == "cancel-task" {
				source += `const receipt=await cancel_task({agentId:a.agentId,commandId});
const result=await wait_task({agentId:a.agentId,commandId});
if(receipt.status!=="accepted"||receipt.commandId!==commandId||result.state!=="cancelled")throw Error(JSON.stringify({receipt,result}));console.log("cancelled");`
			} else {
				source += `await new Promise(()=>{});`
			}
			args := []string{"eval", source}
			if stop == "timeout" {
				args = []string{"eval", "--timeout=1500ms", source}
			}
			process := startProcessIOAgentCLI(t, work, agentCLIEnv(t, server.URL), args...)
			readyCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			waitAgentFile(t, readyCtx, readyFile)
			cancel()
			if stop == "SIGINT" {
				if err := process.cmd.Process.Signal(os.Interrupt); err != nil {
					t.Fatal(err)
				}
			}
			err := process.wait(t, 5*time.Second)
			if stop == "cancel-task" && err != nil {
				t.Fatalf("cancel_task: %v %s", err, process.stderr.String())
			}
			if stop != "cancel-task" && err == nil {
				t.Fatal("canceled parent succeeded")
			}
			if calls.Load() != 1 {
				t.Fatalf("model calls=%d", calls.Load())
			}
		})
	}
}

func TestLowLevelCancellationInCLI(t *testing.T) {
	for _, operation := range []string{"accept", "read", "write", "signal"} {
		for _, stop := range []string{"timeout", "SIGINT"} {
			t.Run(operation+"/"+stop, func(t *testing.T) {
				work := t.TempDir()
				var peer net.Conn
				var accepted chan net.Conn
				var source string
				switch operation {
				case "accept":
					source = `import {listen} from "noe:sockets";const l=await listen({network:"unix",path:"./waiting.sock"});console.log("READY");await l.accept();`
				case "signal":
					source = `import {subscribe} from "noe:signals";const s=subscribe("SIGINT");console.log("READY");await s.next();await new Promise(()=>{});`
				default:
					listener, err := net.Listen("tcp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					defer listener.Close()
					accepted = make(chan net.Conn, 1)
					go func() { c, _ := listener.Accept(); accepted <- c }()
					source = `import {connect} from "noe:sockets";const c=await connect({network:"tcp",host:"127.0.0.1",port:` + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port) + `});console.log("READY");`
					if operation == "read" {
						source += `await c.readable.getReader().read();`
					} else {
						source += `await c.writable.getWriter().write(new Uint8Array(32*1024*1024));`
					}
				}
				args := []string{"eval"}
				if stop == "timeout" {
					args = append(args, "--timeout=750ms")
				}
				process := startCLIWithOpenStdin(t, work, append(args, source)...)
				if !process.stdout.waitFor("READY\n", 3*time.Second) {
					t.Fatalf("operation did not start: %s", process.stderr.String())
				}
				if accepted != nil {
					select {
					case peer = <-accepted:
						if peer == nil {
							t.Fatal("accept failed")
						}
						defer peer.Close()
					case <-time.After(2 * time.Second):
						t.Fatal("server did not accept")
					}
					if operation == "write" {
						_ = peer.SetReadDeadline(time.Now().Add(2 * time.Second))
						if _, err := io.ReadFull(peer, make([]byte, 1)); err != nil {
							t.Fatalf("native write did not begin: %v", err)
						}
					}
				}
				if stop == "SIGINT" {
					if err := process.cmd.Process.Signal(os.Interrupt); err != nil {
						t.Fatal(err)
					}
				}
				got := process.wait(t, 4*time.Second)
				if stop == "timeout" {
					requireExit(t, got, 124)
				} else {
					requireExit(t, got, 1)
				}
				if operation == "accept" {
					if _, err := os.Lstat(filepath.Join(work, "waiting.sock")); !os.IsNotExist(err) {
						t.Fatalf("normal cancellation left socket path: %v", err)
					}
				}
			})
		}
	}
}

func TestLowLevelSignalKeepsCLIAliveAndRestoresDefault(t *testing.T) {
	work := t.TempDir()
	// No timer keeps this script running. close resolves all pending next calls.
	process := startCLIWithOpenStdin(t, work, "eval", `import {subscribe} from "noe:signals";
const s=subscribe("SIGUSR1");console.log("READY");
const first=await s.next();if(first.value!=="SIGUSR1"||first.done)throw Error("signal");
const pending=s.next();await s.close();if(!(await pending).done)throw Error("pending next");console.log("CLOSED");`)
	if !process.stdout.waitFor("READY\n", 3*time.Second) {
		t.Fatal(process.stderr.String())
	}
	if err := process.cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	got := process.wait(t, 3*time.Second)
	requireExit(t, got, 0)
	if got.stdout != "READY\nCLOSED\n" {
		t.Fatalf("signal output: %+v", got)
	}

	// SIGTERM with no remaining subscription must regain its default behavior.
	process = startCLIWithOpenStdin(t, work, "eval", `import {subscribe} from "noe:signals";
const s=subscribe("SIGTERM");await s.close();console.log("UNSUBSCRIBED");
import {stdin} from "noe:io";await stdin.getReader().read();`)
	if !process.stdout.waitFor("UNSUBSCRIBED\n", 3*time.Second) {
		t.Fatal(process.stderr.String())
	}
	if err := process.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	got = process.wait(t, 3*time.Second)
	if got.exitCode != -1 {
		t.Fatalf("default SIGTERM not restored: %+v", got)
	}
}

func TestLowLevelForcedKillLeavesUnixPath(t *testing.T) {
	work := t.TempDir()
	process := startCLIWithOpenStdin(t, work, "eval", `import {listen} from "noe:sockets";await listen({network:"unix",path:"./killed.sock"});console.log("READY");`)
	if !process.stdout.waitFor("READY\n", 3*time.Second) {
		t.Fatal(process.stderr.String())
	}
	if err := process.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = process.wait(t, 3*time.Second)
	info, err := os.Lstat(filepath.Join(work, "killed.sock"))
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("killed listener path: info=%v err=%v", info, err)
	}
}

func TestLowLevelWorkerTerminationClosesOnlyWorkerConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() { c, _ := listener.Accept(); accepted <- c }()
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "worker.js"), `import {connect} from "noe:sockets";import {parentPort} from "node:worker_threads";
const c=await connect({network:"tcp",host:"127.0.0.1",port:`+strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)+`});parentPort.postMessage("READY");await c.readable.getReader().read();`)
	main := `import {Worker} from "node:worker_threads";import {listen,connect} from "noe:sockets";
const main=await listen({network:"tcp",host:"127.0.0.1",port:0});const w=new Worker("./worker.js");
await new Promise((resolve,reject)=>{w.on("message",resolve);w.on("error",reject)});await w.terminate();
const c=await connect({network:"tcp",host:"127.0.0.1",port:main.address.port}), peer=await main.accept();
try{const writer=peer.writable.getWriter();await writer.write(new TextEncoder().encode("main alive"));await writer.close();
if(await new Response(c.readable).text()!=="main alive")throw Error("main listener damaged");}
finally{await c.close();await peer.close();}
console.log(JSON.stringify(main.address));await main.close();`
	got := runCLI(t, work, nil, "eval", main)
	requireExit(t, got, 0)
	var addr map[string]any
	if json.Unmarshal([]byte(got.stdout), &addr) != nil || !strings.Contains(got.stdout, "port") {
		t.Fatalf("main listener damaged: %+v", got)
	}
	select {
	case peer := <-accepted:
		if peer == nil {
			t.Fatal("accept failed")
		}
		defer peer.Close()
		_ = peer.SetReadDeadline(time.Now().Add(time.Second))
		if n, err := peer.Read(make([]byte, 1)); n != 0 || err != io.EOF {
			t.Fatalf("worker socket survived terminate: n=%d err=%v", n, err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker never connected")
	}
}
