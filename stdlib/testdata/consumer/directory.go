package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func directoryIteration(ctx context.Context) error {
	root, err := os.MkdirTemp("", "noe-consumer-directory-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	if err := os.WriteFile(filepath.Join(root, "source"), []byte("directory-consumer"), 0o600); err != nil {
		return err
	}
	worker := `import {parentPort} from "node:worker_threads";
import {iterateDir,copyFile,readFile} from "noe:fs";
await copyFile("source","worker-copy");
const names=[];for await(const entry of iterateDir(".",{batchSize:1})){if(!entry.isFile())throw Error("worker type");names.push(entry.name);}
parentPort.postMessage({names:names.sort(),copy:await readFile("worker-copy","utf8")});parentPort.close();`
	if err := os.WriteFile(filepath.Join(root, "worker.mjs"), []byte(worker), 0o600); err != nil {
		return err
	}
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[directory consumer]", Virtual: true, Module: true, Code: `
import fs,{iterateDir,copyFile,copyFileSync,readFile,readdir,Dirent} from "noe:fs";
import {Worker} from "node:worker_threads";
await copyFile("source","copy");copyFileSync("copy","copy");
if(await readFile("copy","utf8")!=="directory-consumer")throw Error("copy contents");
const names=[];await using directory=iterateDir(".",{batchSize:2});
for await(const entry of directory){if(!(entry instanceof Dirent)||!entry.isFile()||entry.parentPath!==".")throw Error("directory entry");names.push(entry.name);}
if(JSON.stringify(names.sort())!==JSON.stringify(await readdir("."))||!directory.closed||fs.iterateDir!==iterateDir)throw Error("directory listing");
const early=iterateDir(".");for await(const entry of early){if(!entry.name)throw Error("entry name");break;}if(!early.closed)throw Error("early close");
const reason={cancelled:true},controller=new AbortController();const cancelled=iterateDir(".",{signal:controller.signal});
await cancelled.next();controller.abort(reason);if(await cancelled.next().then(()=>null,error=>error)!==reason)throw Error("cancel reason");
const worker=new Worker("./worker.mjs",{type:"module"});
const answer=await new Promise((resolve,reject)=>{worker.on("message",resolve);worker.on("error",reject)});await worker.terminate();
if(answer.copy!=="directory-consumer"||JSON.stringify(answer.names)!==JSON.stringify(await readdir(".")))throw Error("worker directory/copy");
`}, stdlib.Profile{Environment: stdlib.Environment{Cwd: "/"}, Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root}, Workers: &stdlib.WorkerOptions{Max: 1}}, NewLoader: stdlib.FileLoader(nil)})
	if err != nil || result.Code != 0 {
		return fmt.Errorf("directory consumer result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return nil
}
