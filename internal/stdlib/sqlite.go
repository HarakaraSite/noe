package stdlib

import (
	_ "embed"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed sqlite.js
var sqliteJS string

// installSQLite grants no filesystem authority of its own. Memory databases
// need only the loop, and file databases use the already selected FS backend.
func installSQLite(rt *quickjs.Runtime, cfg Config) error {
	native, err := newSQLiteHost(rt, cfg.Loop, cfg.FS)
	if err != nil {
		return err
	}
	if err := native.Set("brand", func(value quickjs.Value) { value.SetCloneBrand(quickjs.CloneUnsupported) }); err != nil {
		return err
	}
	api, err := evalWithHost(rt, "<sqlite>", sqliteJS, native)
	if err != nil {
		return err
	}
	open, err := api.Get("open")
	if err != nil {
		return err
	}
	return rt.SetModuleValues("noe:sqlite", map[string]quickjs.Value{"open": open})
}
