//go:build e2e

package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var cliPath string
var repoRootPath string

func TestMain(m *testing.M) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "cannot locate E2E test source")
		os.Exit(1)
	}
	repoRoot := filepath.Dir(filepath.Dir(sourceFile))
	repoRootPath = repoRoot
	buildDir, err := os.MkdirTemp("", "noe-e2e-build-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create CLI build directory:", err)
		os.Exit(1)
	}
	cliPath = filepath.Join(buildDir, "noe")
	build := exec.Command("go", "build", "-o", cliPath, "./cmd/noe")
	build.Dir = repoRoot
	build.Env = envWith(os.Environ(), "CGO_ENABLED", "0")
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build noe CLI: %v\n%s", err, output)
		_ = os.RemoveAll(buildDir)
		os.Exit(1)
	}
	code := m.Run()
	if err := os.RemoveAll(buildDir); err != nil {
		fmt.Fprintln(os.Stderr, "remove CLI build directory:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
