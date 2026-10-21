package stdlib_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestNodeCommandsShareSelectedCwdEnvAndRelativePATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	defaultBin := filepath.Join(root, "default-bin")
	overrideBin := filepath.Join(root, "override-bin")
	for _, dir := range []string{work, defaultBin, overrideBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	defaultCommand := writeCommandFixture(t, defaultBin, "default")
	writeCommandFixture(t, overrideBin, "override")
	t.Setenv("NOE_COMMAND_HOST_SECRET", "ambient")

	config := stdlib.Config{Run: &stdlib.Run{
		Dir:   "/unused-default",
		Env:   map[string]string{"PATH": filepath.Join("..", "default-bin"), "MARK": "selected"},
		Allow: func(string, []string) error { return nil },
		ResolveDirectory: func(cwd string) (string, error) {
			if cwd != "/virtual" {
				return "", fmt.Errorf("unexpected virtual cwd %q", cwd)
			}
			return work, nil
		},
	}}
	commandJSON, _ := json.Marshal(defaultCommand)
	out, _ := run(t, config, fmt.Sprintf(`
		(async () => {
			const cp = (await import("node:child_process")).default;
			const selected = cp.execFileSync("probe", [], {cwd:"/virtual"}).trim();
			const spawned = cp.spawnSync("probe", [], {cwd:"/virtual", env:{PATH:"../override-bin", MARK:"spawn"}}).stdout.trim();
			const shell = cp.execSync("probe", {cwd:"/virtual", env:{PATH:"../override-bin", MARK:"shell"}}).trim();
			const asyncResult = await cp.execFile("probe", [], {cwd:"/virtual", env:{PATH:"../override-bin", MARK:"async"}});
			const empty = cp.execFileSync(%s, [], {cwd:"/virtual", env:{}}).trim();
			console.log([selected, spawned, shell, asyncResult.stdout.trim(), empty].join("\n"));
		})();
	`, commandJSON))
	want := strings.Join([]string{
		filepath.Clean(work) + "|default|selected|unset",
		filepath.Clean(work) + "|override|spawn|unset",
		filepath.Clean(work) + "|override|shell|unset",
		filepath.Clean(work) + "|override|async|unset",
		filepath.Clean(work) + "|default|unset|unset",
	}, "\n")
	if out != want {
		t.Fatalf("Node command results =\n%s\nwant\n%s", out, want)
	}
}

func writeCommandFixture(t *testing.T, dir, value string) string {
	t.Helper()
	path := filepath.Join(dir, "probe")
	contents := "#!/bin/sh\nprintf '%s|%s|%s|%s\\n' \"$PWD\" \"${FIXTURE_VALUE-unset}\" \"${MARK-unset}\" \"${NOE_COMMAND_HOST_SECRET-unset}\"\n"
	contents = strings.ReplaceAll(contents, "${FIXTURE_VALUE-unset}", value)
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
