package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func watchFilesystem(ctx context.Context) error {
	root, err := os.MkdirTemp("", "noe-consumer-watch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	path := filepath.Join(root, "config")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		return err
	}
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[watch consumer]", Virtual: true, Module: true, Code: `
import fs from "node:fs";
import {watch} from "node:fs/promises";
let denied;try{fs.writeFileSync("config","denied")}catch(error){denied=error}
if(denied?.code!=="ERR_READ_ONLY")throw Error("watch profile lost read-only policy");
{
 await using events=watch("config");
 const pending=events.next();
 mutateWatchedFile();
 const event=await pending;
 if(event.done||event.value.filename!=="config"||event.value.eventType!=="change")throw Error("watch event");
}
const controller=new AbortController(),reason={caller:true};
await using canceled=watch(".",{signal:AbortSignal.any([controller.signal])});
const pending=canceled.next().then(()=>null,error=>error);controller.abort(reason);
const error=await pending;
if(error?.name!=="AbortError"||error.cause!==reason||!(await canceled.next()).done)throw Error("watch cancellation");
`}, stdlib.Profile{
		Environment: stdlib.Environment{Cwd: "/"},
		Modules:     stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: true}},
		Initialize: func(_ context.Context, bindings *stdlib.Bindings) (func(), error) {
			return nil, bindings.Runtime().Set("mutateWatchedFile", func() error {
				return os.WriteFile(path, []byte("after"), 0o600)
			})
		},
	})
	if err != nil || result.Code != 0 {
		return fmt.Errorf("watch consumer result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return nil
}
