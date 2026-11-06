// Copied outside the checkout to exercise only the public Go API.
package main

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--noe-consumer-command" {
		if _, err := os.Stdout.Write([]byte{0, 255, 128, 10}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := advanced(ctx); err != nil {
		return err
	}
	if err := websocketHeaders(ctx); err != nil {
		return err
	}
	if err := eventSource(ctx); err != nil {
		return err
	}
	if err := sqlite(ctx); err != nil {
		return err
	}
	if err := urlImports(ctx); err != nil {
		return err
	}
	if err := commands(ctx); err != nil {
		return err
	}
	if err := stdio(ctx); err != nil {
		return err
	}
	if err := watchFilesystem(ctx); err != nil {
		return err
	}
	if err := dataImports(ctx); err != nil {
		return err
	}
	if err := mappedExecution(ctx); err != nil {
		return err
	}
	root, err := os.MkdirTemp("", "noe-public-stdlib-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	if err := os.WriteFile(filepath.Join(root, "value.mjs"), []byte(`export const value=42;`), 0600); err != nil {
		return err
	}
	worker := `import {parentPort} from "node:worker_threads";import {value} from "value";import {stdin} from "node:process";if(stdin!==process.stdin)throw Error("worker stdin identity");const reader=stdin.getReader();try{if(!(await reader.read()).done)throw Error("worker stdin");}finally{reader.releaseLock();}parentPort.postMessage(value);parentPort.close();`
	if err := os.WriteFile(filepath.Join(root, "worker.mjs"), []byte(worker), 0600); err != nil {
		return err
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "loopback response") }))
	defer server.Close()
	input := &borrowedInput{reader: strings.NewReader("borrowed input")}
	sink := new(borrowedSink)
	initialized, cleaned := 0, 0
	var mu sync.Mutex
	profile := stdlib.Profile{
		Environment: stdlib.Environment{Cwd: "/", Args: []string{"consumer", "entry"}, Env: map[string]string{"SELECTED": "yes"}, Stdin: input, Stdout: sink, Stderr: sink},
		Modules: stdlib.Modules{ProcessInfo: true, Stdio: true, FS: &stdlib.FileSystemOptions{Root: root}, Fetch: &stdlib.FetchOptions{Client: server.Client(), Allow: func(r *http.Request) error {
			if !strings.HasPrefix(r.URL.String(), server.URL) {
				return errors.New("unexpected destination")
			}
			return nil
		}}, Workers: &stdlib.WorkerOptions{Max: 1}},
		NewLoader: stdlib.FileLoader(map[string]string{"value": "./value.mjs"}),
		Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
			mu.Lock()
			initialized++
			mu.Unlock()
			if b.FileSystem() == nil {
				return nil, errors.New("missing actual FS")
			}
			if err := b.Runtime().Set("endpoint", server.URL); err != nil {
				return nil, err
			}
			return func() { mu.Lock(); cleaned++; mu.Unlock() }, nil
		},
	}
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[consumer]", Virtual: true, Module: true, Code: `
import {value} from "value";import {Worker} from "node:worker_threads";import {docs} from "noe:docs";
import {open,readdir} from "node:fs/promises";
if(value!==42||process.env.SELECTED!=="yes"||process.cwd()!=="/"||await new Response(process.stdin).text()!=="borrowed input")throw Error("environment");
const missing=await open("missing.mjs").then(()=>null,error=>error);if(missing?.code!=="ENOENT"||typeof missing.errno!=="number"||missing.errno<=0)throw Error("filesystem error normalization");
const file=await open("value.mjs");try{const buffer=new Uint8Array(6);const result=await file.read(buffer);if(result.bytesRead!==6||result.buffer!==buffer)throw Error("standard FS handle");}finally{await file.close();}
const created=await open("created.bin","wx+",0o600);
try{const exists=await open("created.bin","wx").then(()=>null,error=>error);if(exists?.code!=="EEXIST"||typeof exists.errno!=="number"||exists.errno<=0)throw Error("existing-file error normalization");const metadata=await created.stat();if(!(metadata.mtime instanceof Date)||metadata.mtime.getTime()!==Math.trunc(metadata.mtimeMs)||(metadata.mode&0o170000)!==0o100000)throw Error("FileHandle metadata normalization");await created.write("data");await created.truncate(2);await created.sync();if((await created.stat()).size!==2)throw Error("FileHandle metadata");}finally{await created.close();}
let scopedFile;
{await using owned=await open("scoped.bin","wx+");scopedFile=owned;await owned.write("scoped");}
if((await scopedFile.stat().then(()=>null,error=>error))?.code!=="ERR_FILE_CLOSED")throw Error("await using file disposal");
const entries=await readdir("/",{withFileTypes:true});if(!entries.some(entry=>entry.name==="created.bin"&&entry.isFile()))throw Error("Dirent consumer");
if(await (await fetch(endpoint)).text()!=="loopback response")throw Error("fetch");
const worker=new Worker("./worker.mjs",{type:"module"});const answer=await new Promise((resolve,reject)=>{worker.on("message",resolve);worker.on("error",reject)});await worker.terminate();
if(answer!==42||!docs().includes("noe:agent"))throw Error("worker or full docs");console.log("managed consumer");`}, profile)
	if err != nil || result.Code != 0 {
		return fmt.Errorf("managed result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	mu.Lock()
	balanced := initialized == 2 && cleaned == 2
	mu.Unlock()
	if !balanced || sink.closed || input.closed || !strings.Contains(sink.String(), "managed consumer") {
		return errors.New("execution ownership or initializer cleanup failed")
	}
	_, err = sink.Write([]byte("caller retains output"))
	return err
}

func commands(ctx context.Context) error {
	program, err := os.Executable()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(program)
	if err != nil {
		return err
	}
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[command consumer]", Virtual: true, Module: true, Code: `
import commands, {spawn,execFile} from "node:child_process";
import bare, {spawn as bareSpawn} from "child_process";
if (spawn!==bareSpawn || spawn!==commands.spawn || spawn!==bare.spawn) throw Error("spawn aliases");
const program=` + string(encoded) + `, args=["--noe-consumer-command"];
await using child=await spawn(program,args,{stdin:"ignore",stderr:"ignore"});
try {
  const bytes=new Uint8Array(await new Response(child.stdout).arrayBuffer());
  if (Array.from(bytes).join(",")!=="0,255,128,10" || (await child.exited).code!==0) throw Error("streaming consumer");
} finally { await child.close(); }
const captured=await execFile(program,args,{encoding:"buffer"});
if (Array.from(captured.stdout).join(",")!=="0,255,128,10") throw Error("captured consumer");
if (typeof process!=="undefined") throw Error("commands installed metadata");
if (await import("noe:process").then(()=>true,()=>false)) throw Error("removed process module");
`}, stdlib.Profile{Modules: stdlib.Modules{Commands: &stdlib.CommandOptions{Allow: func(name string, _ []string) error {
		if name != program {
			return errors.New("unexpected command")
		}
		return nil
	}}}})
	if err != nil || result.Code != 0 {
		return fmt.Errorf("command consumer result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return nil
}

func stdio(ctx context.Context) error {
	input := &borrowedInput{reader: strings.NewReader("stdio input")}
	output := new(borrowedSink)
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[stdio consumer]", Virtual: true, Module: true, Code: `
import selected, {stdin,stdout,stderr} from "node:process";
import bare, {stdin as bareInput,stdout as bareOutput} from "process";
if (selected!==bare || stdin!==bareInput || stdout!==bareOutput || selected.stdin!==stdin || selected.stdout!==stdout || selected.stderr!==stderr)
  throw Error("stdio aliases");
if (typeof process!=="undefined" || "env" in selected || "argv" in selected) throw Error("stdio metadata selection");
const stop=new AbortController(),reason={source:"consumer"};
const combined=AbortSignal.any([stop.signal]);
await stdin.pipeTo(stdout,{preventClose:true,signal:combined});
stop.abort(reason);
if(!combined.aborted || combined.reason!==reason) throw Error("combined signal consumer");
const writer=stdout.getWriter();
try { await writer.write(new TextEncoder().encode(" tail")); } finally { writer.releaseLock(); }
if (await import("noe:io").then(()=>true,()=>false)) throw Error("removed stdio module");
`}, stdlib.Profile{Environment: stdlib.Environment{Stdin: input, Stdout: output}, Modules: stdlib.Modules{Stdio: true}})
	if err != nil || result.Code != 0 || output.String() != "stdio input tail" || input.closed || output.closed {
		return fmt.Errorf("stdio consumer result=%+v output=%q error=%s", result, output.String(), stdlib.Diagnostic(err))
	}
	return nil
}

func advanced(ctx context.Context) error {
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	defer rt.Close()
	sink := new(borrowedSink)
	input := &borrowedInput{}
	b, err := stdlib.Attach(rt, loop, stdlib.Profile{Environment: stdlib.Environment{Stdout: sink, Stderr: sink, Stdin: input}})
	if err != nil {
		return err
	}
	source, err := b.Prepare(stdlib.Source{Name: "[advanced]", Virtual: true, Module: true, Code: `import {docs} from "noe:docs";if(!docs().includes("noe:agent"))throw Error("full catalog missing");if(typeof fetch!=="undefined")throw Error("unselected fetch");console.log("advanced consumer");`})
	if err != nil {
		return err
	}
	if _, err = rt.EvalModuleContext(ctx, source.Name, source.Code); err != nil {
		return err
	}
	if err = loop.Run(ctx); err != nil {
		return err
	}
	if err = b.Close(); err != nil {
		return err
	}
	loop.Post(func() { _ = rt.Set("callerPosted", true) })
	if err = loop.Run(ctx); err != nil {
		return err
	}
	value, err := rt.EvalContext(ctx, `callerPosted===true`)
	if err != nil || !value.Bool() || sink.closed || input.closed || input.reads != 0 {
		return fmt.Errorf("advanced caller lifetime: %v", err)
	}
	return nil
}

type borrowedInput struct {
	reader *strings.Reader
	closed bool
	reads  int
}

func (r *borrowedInput) Read(p []byte) (int, error) {
	r.reads++
	if r.reader == nil {
		return 0, io.EOF
	}
	return r.reader.Read(p)
}
func (r *borrowedInput) ReadContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return r.Read(p)
}
func (r *borrowedInput) Close() error { r.closed = true; return nil }

type borrowedSink struct {
	mu     sync.Mutex
	text   strings.Builder
	closed bool
}

func (s *borrowedSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text.Write(p)
}
func (s *borrowedSink) WriteContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.Write(p)
}
func (s *borrowedSink) Close() error   { s.closed = true; return nil }
func (s *borrowedSink) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.text.String() }
