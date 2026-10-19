package agent

import (
	"path/filepath"
	"reflect"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"
)

func TestNormalizeHookModulesBindsHostPathsAndImportMap(t *testing.T) {
	cwd := t.TempDir()
	imports, err := modulemap.Parse([]byte(`{"imports":{"project-hook":"./mapped/hook.mjs"}}`), cwd)
	if err != nil {
		t.Fatal(err)
	}
	got, err := normalizeHookModules([]string{"./hooks/local.js", "project-hook"}, cwd, imports)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(cwd, "hooks", "local.js"),
		filepath.Join(cwd, "mapped", "hook.mjs"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized hooks = %#v, want %#v", got, want)
	}
	if got, err := normalizeHookModules(nil, cwd, imports); err != nil || got != nil {
		t.Fatalf("empty hooks = %#v, error %v", got, err)
	}
}

func TestNormalizeHookModulesRejectsInvalidRegistrationButDoesNotStat(t *testing.T) {
	cwd := t.TempDir()
	for _, specifier := range []string{"", "package-without-map", "./hook.json"} {
		t.Run(specifier, func(t *testing.T) {
			if _, err := normalizeHookModules([]string{specifier}, cwd, nil); err == nil {
				t.Fatalf("normalizeHookModules(%q) succeeded", specifier)
			}
		})
	}
	missing := filepath.Join(cwd, "missing.mjs")
	got, err := normalizeHookModules([]string{missing}, cwd, nil)
	if err != nil || !reflect.DeepEqual(got, []string{missing}) {
		t.Fatalf("missing module was not deferred to task loading: hooks=%#v error=%v", got, err)
	}
}
