//go:build e2e && (agent_live || decision_live || hooks_live)

package e2e_test

import (
	"debug/buildinfo"
	"testing"
)

// Report the dependency embedded in the CLI exercised by the live tests.
func cliDependencyVersion(t *testing.T, modulePath string) string {
	t.Helper()
	info, err := buildinfo.ReadFile(cliPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, dependency := range info.Deps {
		if dependency.Path != modulePath {
			continue
		}
		version := dependency.Path + " " + dependency.Version
		if replacement := dependency.Replace; replacement != nil {
			version += " => " + replacement.Path + " " + replacement.Version
		}
		return version
	}
	t.Fatalf("CLI dependency %q not found", modulePath)
	return ""
}
