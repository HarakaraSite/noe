package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"noe/internal/agent"
	"noe/internal/agent/provider"
)

var runtimeTestExecutable string

func TestMain(m *testing.M) {
	if os.Getenv("NOE_PROCESS_IO_HELPER") != "" {
		for _, arg := range os.Args[1:] {
			if arg == "-test.run=^TestNoeProcessIOHelper$" {
				runProcessIOHelper()
			}
		}
	}
	// The stdlib command fixture re-executes this test binary. Handle it before
	// m.Run and before building the agent executable for the actual suite.
	if os.Getenv("NOE_STDLIB_COMMAND_HELPER") == "1" {
		for _, arg := range os.Args[1:] {
			if arg == "-test.run=^TestStdlibCommandHelper$" {
				fmt.Print("child ready")
				os.Exit(0)
			}
		}
	}
	work, err := os.MkdirTemp("", "noe-runtime-agent-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create runtime test workdir:", err)
		os.Exit(1)
	}
	binary := filepath.Join(work, "noe")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err == nil {
		var output []byte
		cmd := exec.Command("go", "build", "-o", binary, "./cmd/noe")
		cmd.Dir = root
		output, err = cmd.CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "build NOE runtime test executable: %v\n%s", err, output)
		}
	}
	if err != nil {
		_ = os.RemoveAll(work)
		os.Exit(1)
	}
	runtimeTestExecutable = binary
	code := m.Run()
	if err := os.RemoveAll(work); err != nil {
		fmt.Fprintln(os.Stderr, "remove runtime test workdir:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func agentTestOptions(p provider.Provider) *agent.Options {
	return &agent.Options{Provider: p, Executable: runtimeTestExecutable}
}
