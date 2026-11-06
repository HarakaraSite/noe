package stdlib

import (
	"sort"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// ProcessModuleOptions composes independently selected process capabilities.
// Processes must install the metadata object first when ProcessInfo is true.
// Stdio and Spawn are the execution-owned native adapters, when selected.
type ProcessModuleOptions struct {
	ProcessInfo bool
	Commands    *Run
	Stdio       map[string]quickjs.Value
	Spawn       quickjs.Value
}

// ProcessModule publishes noe:process before user modules are evaluated. Its
// default is a namespace of the selected named exports; the metadata object
// remains separate so commands never become properties of global process.
func ProcessModule(rt *quickjs.Runtime, opts ProcessModuleOptions) error {
	if !opts.ProcessInfo && opts.Commands == nil && len(opts.Stdio) == 0 {
		return nil
	}
	values := make(map[string]quickjs.Value)
	if opts.ProcessInfo {
		p, err := rt.Get("process")
		if err != nil {
			return err
		}
		values["process"] = p
		for name, stream := range opts.Stdio {
			if err := p.Set(name, stream); err != nil {
				return err
			}
		}
	}
	if opts.Commands != nil {
		for name, value := range commandExports(rt, opts.Commands, opts.Spawn) {
			encoded, err := rt.Encode(value)
			if err != nil {
				return err
			}
			values[name] = encoded
		}
	}
	for name, stream := range opts.Stdio {
		values[name] = stream
	}
	api := rt.NewObject()
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := api.Set(name, values[name]); err != nil {
			return err
		}
	}
	values["default"] = api
	return rt.SetModuleValues("noe:process", values)
}
