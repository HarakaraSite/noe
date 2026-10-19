package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"
)

// normalizeHookModules binds registered hook specifiers to absolute module
// IDs. The synthetic referrer anchors relative specifiers to the host cwd;
// the module loader later resolves imports from each hook's real file ID.
func normalizeHookModules(specifiers []string, cwd string, importMap *modulemap.Snapshot) ([]string, error) {
	if len(specifiers) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(cwd) == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve hook host directory: %w", err)
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve hook host directory: %w", err)
	}
	referrer := filepath.Join(cwd, "<noe-hook-config>.js")
	resolved := make([]string, 0, len(specifiers))
	for index, specifier := range specifiers {
		if strings.TrimSpace(specifier) == "" {
			return nil, fmt.Errorf("hook module %d must be a nonempty module specifier", index)
		}
		moduleID, err := importMap.Resolve(specifier, referrer)
		if err != nil {
			return nil, fmt.Errorf("resolve hook module %q: %w", specifier, err)
		}
		extension := filepath.Ext(moduleID)
		if extension != ".js" && extension != ".mjs" {
			return nil, fmt.Errorf("hook module %q must resolve to a .js or .mjs file", specifier)
		}
		resolved = append(resolved, moduleID)
	}
	return resolved, nil
}
