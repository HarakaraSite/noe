package stdlib_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestDirectoryIterationEntriesLazinessAndConcurrentReads(t *testing.T) {
	root := t.TempDir()
	for i := range 259 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("entry-%03d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	links := os.Symlink("entry-000", filepath.Join(root, "link")) == nil
	out, _ := run(t, stdlib.Config{FS: &stdlib.FS{Root: root}}, fmt.Sprintf(`(async()=>{
 const fs=await import("noe:fs");
 if(fs.default.iterateDir!==fs.iterateDir)throw Error("default export");
 const lazy=fs.iterateDir("later",{batchSize:3});
 await fs.mkdir("later");await fs.writeFile("later/created","yes");
 if((await lazy.next()).value.name!=="created"||!(await lazy.next()).done||!lazy.closed)throw Error("lazy open or EOF");
 const iterator=fs.iterateDir("/",{batchSize:7});
 const entries=[];
 const first=await Promise.all([iterator.next(),iterator.next(),iterator.next()]);
 for(const item of first){if(item.done)throw Error("unexpected done");entries.push(item.value);}
 for await(const item of iterator)entries.push(item);
 if(!iterator.closed||!(await iterator.next()).done)throw Error("EOF lifetime");
 const names=entries.map(e=>e.name).sort();
 if(JSON.stringify(names)!==JSON.stringify(await fs.readdir("/")))throw Error("listing differs");
 if(new Set(names).size!==entries.length)throw Error("duplicate entries");
 for(const entry of entries)if(!(entry instanceof fs.Dirent)||entry.parentPath!=="/")throw Error("Dirent contract");
 const byName=Object.fromEntries(entries.map(e=>[e.name,e]));
 if(!byName.empty.isDirectory()||!byName["entry-000"].isFile())throw Error("entry type");
 if(%t&&(!byName.link.isSymbolicLink()||byName.link.isFile()))throw Error("followed final symlink");
 const empty=fs.iterateDir("empty");if(!(await empty.next()).done||!empty.closed)throw Error("empty directory");
 const early=fs.iterateDir("/");for await(const entry of early){if(!entry.name)throw Error("name");break;}
 if(!early.closed||!(await early.next()).done)throw Error("break did not close");
 let disposed;{await using iterator=fs.iterateDir("/",{batchSize:1});disposed=iterator;await iterator.next();}
 if(!disposed.closed||!(await disposed.next()).done)throw Error("async disposal");
 const unused=fs.iterateDir("missing");await unused.close();await unused.close();
 if(!(await unused.next()).done||(await unused.return(42)).value!==42)throw Error("close before open");
 console.log("ok");
})()`, links))
	if out != "ok" {
		t.Fatalf("directory iteration output=%q", out)
	}
}

func TestDirectoryIterationCancellationAndValidation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"one", "two", "three"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out, _ := run(t, stdlib.Config{FS: &stdlib.FS{Root: root, ReadOnly: true}}, `(async()=>{
 const {iterateDir}=await import("noe:fs");
 for(const path of ["",null,42,"bad\0name"]){let error;try{iterateDir(path)}catch(e){error=e}if(error?.code!=="ERR_INVALID_ARG")throw Error("path validation");}
 for(const options of [null,1,[],{batchSize:0},{batchSize:-1},{batchSize:1.5},{batchSize:Infinity},{batchSize:Number.MAX_SAFE_INTEGER+1}]){
  let error;try{iterateDir("/",options)}catch(e){error=e}if(error?.code!=="ERR_INVALID_ARG")throw Error("option validation");
 }
 for(const signal of [null,{}, {aborted:false,addEventListener(){}}]){let rejected=false;try{iterateDir("/",{signal})}catch{rejected=true}if(!rejected)throw Error("fake signal");}
 const expect=async(iterator,code)=>{const error=await iterator.next().then(()=>null,e=>e);if(error?.code!==code||!iterator.closed||!(await iterator.next()).done)throw Error("expected "+code+": "+error);};
 await expect(iterateDir("missing"),"ENOENT");await expect(iterateDir("one"),"ENOTDIR");
 const reason={cancelled:true};
 const pre=iterateDir("missing",{signal:AbortSignal.abort(reason)});
 if(await pre.next().then(()=>null,e=>e)!==reason||!pre.closed||!(await pre.next()).done)throw Error("pre-aborted reason");
 const controller=new AbortController();const iterator=iterateDir("/",{batchSize:3,signal:controller.signal});
 await iterator.next();controller.signal.dispatchEvent(new Event("abort"));
 if((await iterator.next()).done||iterator.closed)throw Error("synthetic abort cancelled");
 controller.abort(reason);
 if(await iterator.next().then(()=>null,e=>e)!==reason||!iterator.closed||!(await iterator.next()).done)throw Error("buffered cancellation");
 const pendingController=new AbortController();const pending=iterateDir("/",{signal:pendingController.signal});
 const p=pending.next();pendingController.abort(reason);
 if(await p.then(()=>null,e=>e)!==reason)throw Error("pending cancellation");
 await pending.close();
 const closing=iterateDir("/");const first=closing.next(),second=closing.next();await closing.close();
 if(!(await first).done||!(await second).done||!(await closing.next()).done)throw Error("close while reading");
 const huge=iterateDir("/",{batchSize:Number.MAX_SAFE_INTEGER});
 const entries=[];for await(const entry of huge)entries.push(entry);
 if(entries.length!==3)throw Error("large safe batch value");
 console.log("ok");
})()`)
	if out != "ok" {
		t.Fatalf("directory validation output=%q", out)
	}
}

func TestDirectoryIterationRootCwdAndReadOnly(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inside", "value"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	links := os.Symlink(outside, filepath.Join(root, "outward")) == nil
	out, _ := run(t, stdlib.Config{FS: &stdlib.FS{Root: root, Cwd: "/inside", ReadOnly: true}}, fmt.Sprintf(`(async()=>{
 const {iterateDir}=await import("noe:fs");
 const iterator=iterateDir(".");const entry=(await iterator.next()).value;
 if(entry.name!=="value"||entry.parentPath!=="."||!entry.isFile())throw Error("cwd");await iterator.close();
 for(const path of ["../../escape",...(%t?["/outward"]:[])]){
  const iterator=iterateDir(path);const error=await iterator.next().then(()=>null,e=>e);
  if(!error||!iterator.closed)throw Error("root escape");
 }
 console.log("ok");
})()`, links))
	if out != "ok" {
		t.Fatalf("directory authority output=%q", out)
	}
}
