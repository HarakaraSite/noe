package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

type runtimeProviderMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []struct {
		ID string `json:"id"`
	} `json:"tool_calls"`
}

func decodeRuntimeProviderMessages(t *testing.T, messages []json.RawMessage) []runtimeProviderMessage {
	t.Helper()
	decoded := make([]runtimeProviderMessage, 0, len(messages))
	for index, raw := range messages {
		var message runtimeProviderMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			t.Fatalf("decode provider message %d: %v", index, err)
		}
		decoded = append(decoded, message)
	}
	return decoded
}

func runtimeMessageContains(messages []runtimeProviderMessage, role, text string) bool {
	for _, message := range messages {
		if message.Role == role && strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

func runtimeContextStatusMessages(messages []runtimeProviderMessage) []runtimeProviderMessage {
	var statuses []runtimeProviderMessage
	for _, message := range messages {
		if strings.HasPrefix(message.Content, "NOE_CONTEXT_STATUS\n") {
			statuses = append(statuses, message)
		}
	}
	return statuses
}

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
