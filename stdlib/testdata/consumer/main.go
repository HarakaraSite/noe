// Copied outside the checkout to exercise only the public Go API.
package main

import (
	"context"
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
	root, err := os.MkdirTemp("", "noe-public-stdlib-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	if err := os.WriteFile(filepath.Join(root, "value.mjs"), []byte(`export const value=42;`), 0600); err != nil {
		return err
	}
	worker := `import {parentPort} from "node:worker_threads";import {value} from "value";import {stdin} from "noe:io";if(!(await stdin.getReader().read()).done||process.stdin.read()!==null)throw Error("worker stdin");parentPort.postMessage(value);parentPort.close();`
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
import {open} from "noe:fs-handle";
if(value!==42||process.env.SELECTED!=="yes"||process.cwd()!=="/"||process.stdin.read()!=="borrowed input")throw Error("environment");
const file=await open("value.mjs");try{if((await file.readAt(0,6)).bytesRead!==6)throw Error("native FS");}finally{await file.close();}
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
