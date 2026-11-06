package stdlib_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestProfileFilesystemWatchDetachPreservesBorrowedRuntime(t *testing.T) {
	for _, expression := range []string{`watch(".")`, `fs.watch(".")`} {
		t.Run(expression, func(t *testing.T) {
			rt := quickjs.New()
			loop := stdlib.NewLoop(rt)
			rt.OnClose(loop.Close)
			defer rt.Close()
			bindings, err := stdlib.Attach(rt, loop, stdlib.Profile{
				Environment: stdlib.Environment{Cwd: "/"},
				Modules:     stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: t.TempDir(), ReadOnly: true}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer bindings.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			source, err := bindings.Prepare(stdlib.Source{Name: "memory:watch-detach", Virtual: true, Module: true, Code: `
import {watch} from "noe:fs";
import fs from "noe:fs";
globalThis.ownedWatch=` + expression + `;
if(typeof ownedWatch.next==="function")globalThis.pendingWatch=ownedWatch.next();
`})
			if err != nil {
				t.Fatal(err)
			}
			entry, err := rt.EvalModuleContext(ctx, source.Name, source.Code)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Await(ctx); err != nil {
				t.Fatal(err)
			}
			if !rt.Busy() {
				t.Fatal("persistent watch did not keep its runtime alive")
			}
			if err := bindings.Close(); err != nil {
				t.Fatal(err)
			}
			if err := loop.Run(ctx); err != nil {
				t.Fatal(err)
			}
			if rt.Busy() || loop.Context().Err() != nil {
				t.Fatal("watch survived detach or closed the borrowed loop")
			}
			value, err := rt.Eval(`40+2`)
			if err != nil || value.String() != "42" {
				t.Fatalf("borrowed runtime unusable: value=%v error=%v", value, err)
			}
		})
	}
}

func TestProfileFilesystemWatchUsesVirtualCwd(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[virtual watch]", Virtual: true, Module: true, Code: `
import fs from "noe:fs";
import {watch} from "noe:fs";
await using changes=watch(".");
const next=changes.next();fs.writeFileSync("entry","value");
const event=await next;
if(event.done||event.value.filename!=="entry"||!fs.existsSync("/sub/entry")||fs.existsSync("/entry"))throw Error("watch cwd");
`}, stdlib.Profile{
		Environment: stdlib.Environment{Cwd: "/sub"},
		Modules:     stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root}},
	})
	if err != nil || result.Code != 0 {
		t.Fatalf("virtual watch result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}
