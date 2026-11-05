package stdlib

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilesystemErrorCodesAcrossEntryPoints(t *testing.T) {
	cwd := t.TempDir()
	rt, _, loop := fileRuntime(t, cwd)
	evalFiles(t, rt, loop, `import fs from "node:fs";
import * as promises from "node:fs/promises";
const checks=[()=>fs.readFileSync("missing"),()=>fs.statSync("missing"),()=>fs.lstatSync("missing"),()=>fs.readdirSync("missing"),()=>fs.unlinkSync("missing"),()=>fs.realpathSync("missing"),()=>fs.readlinkSync("missing"),()=>fs.copyFileSync("missing","copy"),()=>fs.renameSync("missing","renamed"),()=>fs.mkdirSync("missing/child"),
 ()=>promises.readFile("missing"),()=>promises.stat("missing"),()=>promises.readdir("missing"),()=>promises.unlink("missing"),()=>promises.rename("missing","renamed"),()=>promises.open("missing"),
 ()=>new Response(fs.createReadStream("missing")).text(),()=>fs.createWriteStream("missing/child").getWriter().write(new Uint8Array([1]))];
for(const call of checks){
 let failure;try{await call()}catch(error){failure=error}
 if(!(failure instanceof Error)||failure.code!=="ENOENT"||typeof failure.errno!=="number"||failure.errno<=0||typeof failure.syscall!=="string"||typeof failure.path!=="string")throw Error("error contract "+JSON.stringify({code:failure?.code,errno:failure?.errno,path:failure?.path,message:failure?.message}));
}
try{fs.renameSync("missing","renamed")}catch(error){if(!error.dest.endsWith("renamed"))throw Error("rename destination")}
try{fs.readdirSync(".",{withFileTypes:"bad"});throw Error("invalid options accepted")}catch(error){if(error.code!=="ERR_INVALID_ARG")throw error}
const marker=new Error("getter marker");
const options={get withFileTypes(){throw marker}};
try{fs.readdirSync(".",options)}catch(error){if(error!==marker)throw Error("sync JS exception identity lost")}
try{await promises.readdir(".",options)}catch(error){if(error!==marker)throw Error("promise JS exception identity lost")}
`)
}

func TestFilesystemMetadataDateAndMode(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "file")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(cwd, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2025, 2, 3, 4, 5, 6, 123456789, time.UTC)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	linked := os.Symlink("file", filepath.Join(cwd, "link")) == nil
	rt, _, loop := fileRuntime(t, cwd)
	if err := rt.Set("linked", linked); err != nil {
		t.Fatal(err)
	}
	evalFiles(t, rt, loop, `import fs from "node:fs";
import {stat,lstat,open} from "node:fs/promises";
const f=await open("file");
try{
 const values=[fs.statSync("file"),fs.lstatSync("file"),await stat("file"),await lstat("file"),await f.stat()];
 for(const info of values){
  if(!(info.mtime instanceof Date)||info.mtime.getTime()!==Math.trunc(info.mtimeMs)||!JSON.stringify(info).includes(info.mtime.toISOString()))throw Error("mtime Date contract");
  if((info.mode&0o170000)!==0o100000||!info.isFile()||info.isDirectory()||(info.mode&0o777)>0o600)throw Error("file mode bits");
 }
 if(values.some(info=>info.mtimeMs!==values[0].mtimeMs))throw Error("metadata entry points differ");
 if((fs.statSync("dir").mode&0o170000)!==0o040000)throw Error("directory mode bits");
 if(linked){
  const link=await lstat("link");
  if((link.mode&0o170000)!==0o120000||!link.isSymbolicLink()||(fs.statSync("link").mode&0o170000)!==0o100000)throw Error("link mode bits");
 }
}finally{await f.close()}
`)
}

func TestFilesystemCodedErrorsKeepGoCause(t *testing.T) {
	for _, source := range []string{`fs.readFileSync("missing")`, `fs.promises.readFile("missing")`} {
		t.Run(source, func(t *testing.T) {
			rt, _, loop := fileRuntime(t, t.TempDir())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			importing, err := rt.Eval(`import("node:fs")`)
			if err != nil {
				t.Fatal(err)
			}
			fs, err := importing.Await(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Set("fs", fs); err != nil {
				t.Fatal(err)
			}
			result, err := rt.Eval(source)
			if err == nil {
				err = loop.Run(ctx)
			}
			if err == nil {
				_, err = result.Await(ctx)
			}
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing-file error lost Go cause: %v", err)
			}
		})
	}
}
