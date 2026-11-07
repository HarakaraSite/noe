package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func webStorage(ctx context.Context) error {
	root, err := os.MkdirTemp("", "noe-web-storage-consumer-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	profile := stdlib.Profile{
		Environment: stdlib.Environment{Cwd: "/"},
		Modules: stdlib.Modules{
			FS:           &stdlib.FileSystemOptions{Root: root},
			LocalStorage: &stdlib.LocalStorageOptions{Path: "consumer-state.json"},
			Workers:      &stdlib.WorkerOptions{Max: 1},
		},
	}

	workerCode := `import {parentPort} from "node:worker_threads";
if(sessionStorage.length!==0||localStorage.getItem("main")!=="persisted")throw Error("Worker storage scope");
sessionStorage.setItem("private","worker");localStorage.setItem("worker","shared");
parentPort.postMessage("storage consumer Worker passed");parentPort.close();`
	first := `import {Worker} from "node:worker_threads";
if(typeof Storage!=="function"||!(sessionStorage instanceof Storage)||typeof localStorage!=="object")throw Error("Storage globals missing");
if(sessionStorage.length!==0)throw Error("initial session storage is not empty");
sessionStorage.setItem("private","main");localStorage.setItem("main","persisted");
const worker=new Worker(` + fmt.Sprintf("%q", workerCode) + `,{eval:true,type:"module"});
const result=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});
await worker.terminate();if(result!=="storage consumer Worker passed")throw Error("Worker result");`
	if _, err := runStorageConsumer(ctx, profile, first); err != nil {
		return fmt.Errorf("first Web Storage consumer execution: %w", err)
	}

	second := `if(sessionStorage.length!==0||sessionStorage.getItem("private")!==null)throw Error("session storage persisted between runtimes");
if(localStorage.getItem("main")!=="persisted"||localStorage.getItem("worker")!=="shared")throw Error("local storage did not persist or share with Worker");`
	if _, err := runStorageConsumer(ctx, profile, second); err != nil {
		return fmt.Errorf("second Web Storage consumer execution: %w", err)
	}
	if _, err := os.Stat(filepath.Join(root, "consumer-state.json")); err != nil {
		return fmt.Errorf("public profile did not create selected localStorage file: %w", err)
	}

	_, err = runStorageConsumer(ctx, stdlib.Profile{}, `
if(typeof Storage!=="function"||typeof sessionStorage!=="object"||typeof localStorage!=="undefined")throw Error("default Storage capability profile");
sessionStorage.setItem("only","session");`)
	if err != nil {
		return fmt.Errorf("default Web Storage capability: %w", err)
	}
	return nil
}

func runStorageConsumer(ctx context.Context, profile stdlib.Profile, code string) (stdlib.Result, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[external Web Storage consumer]", Virtual: true, Module: true, Code: code}, profile)
	if err != nil {
		return result, fmt.Errorf("execution failed: %s", stdlib.Diagnostic(err))
	}
	if result.Code != 0 {
		return result, fmt.Errorf("execution returned exit code %d", result.Code)
	}
	return result, nil
}
