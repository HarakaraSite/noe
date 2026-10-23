package stdlib

import quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"

// installHTTP exposes the functions already installed by this configuration.
// Reading existing values preserves their hosts, types, loop and permissions.
func installHTTP(rt *quickjs.Runtime, cfg Config) error {
	exports := make(map[string]quickjs.Value)
	for _, entry := range []struct {
		name    string
		enabled bool
	}{{"fetch", cfg.Fetch != nil}, {"serve", cfg.Serve != nil}} {
		if !entry.enabled {
			continue
		}
		value, err := rt.Get(entry.name)
		if err != nil {
			return err
		}
		exports[entry.name] = value
	}
	if len(exports) == 0 {
		return nil
	}
	return rt.SetModuleValues("noe:http", exports)
}
