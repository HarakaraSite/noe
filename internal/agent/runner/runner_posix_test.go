//go:build !windows

package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUsesOnlyTheExplicitEnvironment(t *testing.T) {
	t.Setenv("NOE_RUNNER_PARENT_ONLY", "ambient-value")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	args := []string{"-test.run=^TestRunnerEnvironmentHelper$"}

	for _, test := range []struct {
		name string
		env  []string
		want string
	}{
		{name: "nil environment is empty", env: nil, want: `path="" ambient="" provided=""`},
		{name: "provided entries are preserved", env: []string{"NOE_RUNNER_ENV_HELPER=1", "NOE_RUNNER_VALUE=provided"}, want: `path="" ambient="" provided="provided"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := Run(context.Background(), Command{
				Executable: executable,
				Args:       args,
				Cwd:        cwd,
				Env:        test.env,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Reason != "completed" || result.ExitCode != 0 {
				t.Fatalf("child result = %#v", result)
			}
			if !strings.Contains(result.Stdout, test.want) {
				t.Fatalf("child output %q does not contain %q", result.Stdout, test.want)
			}
			if !strings.Contains(result.Stdout, "cwd="+filepath.Clean(cwd)) {
				t.Fatalf("child did not use configured cwd: %q", result.Stdout)
			}
		})
	}
}

// TestRunnerEnvironmentHelper is also invoked as a child by the test above.
func TestRunnerEnvironmentHelper(t *testing.T) {
	if !containsArg(os.Args, "-test.run=^TestRunnerEnvironmentHelper$") {
		return
	}
	_, _ = fmt.Fprintf(os.Stdout, "cwd=%s path=%q ambient=%q provided=%q\n",
		mustGetwd(), os.Getenv("PATH"), os.Getenv("NOE_RUNNER_PARENT_ONLY"), os.Getenv("NOE_RUNNER_VALUE"))
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func mustGetwd() string {
	wd, _ := os.Getwd()
	return wd
}
