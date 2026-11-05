package stdlib_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestProfileNodeFilesystemHandlesAgree(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		name := "writable"
		if readOnly {
			name = "readonly"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "sub", "value"), []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			rt := quickjs.New()
			loop := stdlib.NewLoop(rt)
			rt.OnClose(loop.Close)
			defer rt.Close()
			view, err := stdlib.Attach(rt, loop, stdlib.Profile{
				Environment: stdlib.Environment{Cwd: "/sub"},
				Modules:     stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: readOnly}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err := rt.Set("readOnlyCase", readOnly); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			source, err := view.Prepare(stdlib.Source{Name: "memory:fs", Module: true, Virtual: true, Code: `
import fs from "node:fs";
import fsPromises, {open as nodeOpen} from "node:fs/promises";
import barePromises, {open as bareOpen} from "fs/promises";
if (nodeOpen!==bareOpen||nodeOpen!==fs.promises.open||nodeOpen!==fsPromises.open||nodeOpen!==barePromises.open) throw new Error("open aliases differ");
const node=fs.readFileSync("value","utf8");
const file=await nodeOpen("/sub/value");
const buffer=new Uint8Array(3);
const read=await file.read(buffer,0,buffer.byteLength,0);
if (read.buffer!==buffer||read.bytesRead!==3) throw new Error("read result does not retain its buffer");
const handleText=new TextDecoder().decode(read.buffer.subarray(0,read.bytesRead));
await file.close();
if (readOnlyCase) {
  let nodeDenied=false, handleDenied=false;
  try { fs.writeFileSync("value","bad"); } catch(error) { nodeDenied=error.message.includes("read-only"); }
  try { await nodeOpen("value","r+"); } catch(error) { handleDenied=error.code==="ERR_READ_ONLY"; }
  if (!nodeDenied || !handleDenied) throw new Error("inconsistent read-only policy");
} else {
  const writable=await nodeOpen("value","r+");
  await writable.write(new TextEncoder().encode("new"),0,3,0);
  await writable.close();
}
globalThis.fsResult=handleText+"|"+node+"|"+fs.readFileSync("/sub/value","utf8");
`})
			if err != nil {
				t.Fatal(err)
			}
			entry, err := rt.EvalModuleContext(ctx, source.Name, source.Code)
			if err != nil {
				t.Fatal(err)
			}
			if err := loop.Run(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Await(ctx); err != nil {
				t.Fatal(err)
			}
			result, err := rt.Get("fsResult")
			want := "old|old|new"
			expectedBytes := "new"
			if readOnly {
				want = "old|old|old"
				expectedBytes = "old"
			}
			if err != nil || result.String() != want {
				t.Fatalf("filesystem result=%q, want %q (%v)", result.String(), want, err)
			}
			got, err := view.FileSystem().ReadFile(ctx, "value")
			if err != nil || string(got) != expectedBytes {
				t.Fatalf("Go filesystem bytes=%q (%v)", got, err)
			}
		})
	}
}

func TestProfileFilesystemAPIsRejectOutwardSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "value"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	defer rt.Close()
	view, err := stdlib.Attach(rt, loop, stdlib.Profile{Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root}}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	source, err := view.Prepare(stdlib.Source{Name: "memory:links", Module: true, Virtual: true, Code: `
import fs from "node:fs";
import {open} from "node:fs/promises";
let nodeDenied=false,handleDenied=false;
try { fs.readFileSync("out/value"); } catch(error) { nodeDenied=error.message.includes("root"); }
try { await open("out/value"); } catch(error) { handleDenied=error.message.includes("root"); }
if (!nodeDenied||!handleDenied) throw new Error("outward link was followed");
globalThis.linksDenied=true;
`})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := rt.EvalModuleContext(ctx, source.Name, source.Code)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Await(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Get("linksDenied")
	if err != nil || !result.Bool() {
		t.Fatalf("link guard=%v (%v)", result, err)
	}
}
