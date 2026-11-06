package stdlib

import quickjs "github.com/go-quickjs/go-quickjs"

// installHTTP publishes only selected host helpers. The installers supply
// their existing functions directly; standard Web APIs remain globals.
func installHTTP(rt *quickjs.Runtime, exports map[string]quickjs.Value) error {
	if len(exports) == 0 {
		return nil
	}
	return rt.SetModuleValues("noe:http", exports)
}
