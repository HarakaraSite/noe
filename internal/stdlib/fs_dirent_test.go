package stdlib_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestFilesDirentAcrossSyncAndPromises(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	links := false
	if err := os.Symlink(outside, filepath.Join(root, "outward")); err == nil {
		links = true
		if err := os.Symlink("missing", filepath.Join(root, "dangling")); err != nil {
			t.Fatal(err)
		}
	}
	source := fmt.Sprintf(`;(async()=>{
 const fs=await import("node:fs"),bare=await import("fs");
 const promises=await import("node:fs/promises");
 if(fs.Dirent!==fs.default.Dirent||fs.Dirent!==bare.Dirent)throw Error("Dirent aliases differ");
 const sync=fs.readdirSync("/",{withFileTypes:true});
 const async=await promises.readdir("/",{withFileTypes:true});
 const names=fs.readdirSync("/");
 if(JSON.stringify(names)!==JSON.stringify(fs.readdirSync("/","utf8")))throw Error("previous extra-argument behavior changed");
 if(JSON.stringify(names)!==JSON.stringify(sync.map(entry=>entry.name)))throw Error("default names changed");
 if(JSON.stringify(names)!==JSON.stringify(await promises.readdir("/",{withFileTypes:false})))throw Error("promise names changed");
 for(const entries of [sync,async]){
  if(entries.some(entry=>!(entry instanceof fs.Dirent)||entry.parentPath!=="/"))throw Error("Dirent identity/path");
  const byName=Object.fromEntries(entries.map(entry=>[entry.name,entry]));
  if(!byName.file.isFile()||byName.file.isDirectory()||!byName.dir.isDirectory())throw Error("file/directory classification");
  if(byName.file.isFIFO()||byName.file.isSocket()||byName.file.isCharacterDevice()||byName.file.isBlockDevice())throw Error("regular file type predicates");
  if(%t&&(!byName.outward.isSymbolicLink()||!byName.dangling.isSymbolicLink()||byName.outward.isDirectory()))throw Error("directory listing followed links");
 }
 if(fs.readdirSync("/dir",{withFileTypes:true}).length!==0||(await promises.readdir("/dir",{withFileTypes:true})).length!==0)throw Error("empty Dirent listing");
 console.log("ok");
 })()`, links)
	out, _ := run(t, stdlib.Config{FS: &stdlib.FS{Root: root, ReadOnly: true}}, source)
	if out != "ok" {
		t.Fatalf("Dirent output=%q", out)
	}
}
