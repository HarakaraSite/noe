package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func dataImports(ctx context.Context) error {
	root, err := os.MkdirTemp("", "noe-data-import-consumer-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	for name, data := range map[string][]byte{
		"default.json": []byte(`{"value":1}`),
		"scoped.json":  []byte(`{"value":42}`),
		"payload":      {0, 255, 128, 10},
	} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			return err
		}
	}
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "/entry.mjs", Module: true, Code: `
import data from "settings" with {type:"json"};
import * as alias from "assets/scoped.json" with {type:"json"};
import bytes from "assets/payload" with {type:"bytes"};
const text=await import("assets/scoped.json",{with:{type:"text"}});
if(alias.default!==data||data.value!==42||JSON.parse(text.default).value!==42||!bytes.buffer.immutable||Array.from(bytes).join()!=="0,255,128,10")throw Error("typed import consumer");
export default data.value;`}, stdlib.Profile{
		Environment: stdlib.Environment{Cwd: "/"},
		Modules:     stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: true}},
		Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{
			Imports: map[string]string{"settings": "./default.json", "assets/": "./"},
			Scopes:  map[string]map[string]string{"./entry.mjs": {"settings": "./scoped.json"}},
		}),
	})
	if err != nil || string(result.Data) != "42" {
		return fmt.Errorf("data import consumer result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return nil
}
