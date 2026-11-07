//go:build e2e

package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUDPRoundTripExampleThroughCLI(t *testing.T) {
	got := runCLI(t, t.TempDir(), nil, "run", filepath.Join(repoRootPath, "examples/low-level/udp-roundtrip.js"))
	requireExit(t, got, 0)
	if got.stderr != "" || strings.TrimSpace(got.stdout) != "UDP roundtrip passed" {
		t.Fatalf("UDP example: %+v", got)
	}
}
