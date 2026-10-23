package nativeio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/commandenv"
	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestProfileSpawnUsesSharedCommandEnvironmentWithoutProcess(t *testing.T) {
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
	defaultCommand := writeNativeCommandFixture(t, defaultBin, "selected")
	writeNativeCommandFixture(t, overrideBin, "override")
	t.Setenv("NOE_NATIVE_HOST_SECRET", "ambient")

	var allowed []string
	commandConfig := &commandenv.Config{
		Cwd: work,
		Env: map[string]string{"PATH": filepath.Join("..", "default-bin"), "MARK": "default"},
		Allow: func(command string, _ []string) error {
			allowed = append(allowed, command)
			return nil
		},
		ResolveDirectory: func(cwd string) (string, error) {
			if cwd != "/virtual" {
				return "", fmt.Errorf("unexpected virtual cwd %q", cwd)
			}
			return work, nil
		},
	}
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	t.Cleanup(func() { _ = rt.Close() })
	if err := stdlib.Install(rt, stdlib.Config{Loop: loop, Stdout: os.Stdout, Stderr: os.Stderr}); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSelected(rt, Options{Cwd: work, Command: commandConfig}, Modules{Commands: true}); err != nil {
		t.Fatal(err)
	}
	defaultCommandJSON, _ := json.Marshal(defaultCommand)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	script := fmt.Sprintf(`
		import {spawn} from "noe:process";
		const read = async (command, options) => {
			const child = await spawn(command, [], {stdout:"pipe", stderr:"pipe", cwd:"/virtual", ...options});
			const reader = child.stdout.getReader();
			let text = "";
			for (;;) {
				const {done, value} = await reader.read();
				if (done) break;
				text += new TextDecoder().decode(value);
			}
			const status = await child.exited;
			await child.close();
			if (status.code !== 0) throw Error("fixture exited " + status.code);
			return text.trim();
		};
		globalThis.spawnResults = null;
		(async () => {
			const inherited = await read("probe", {});
			const overridden = await read("probe", {env:{PATH:"../override-bin", MARK:"native"}});
			const empty = await read(%s, {env:{}});
			globalThis.spawnResults = [inherited, overridden, empty];
		})();
	`, defaultCommandJSON)
	if _, err := rt.EvalModuleContext(ctx, "profile-spawn.mjs", script); err != nil {
		t.Fatal(err)
	}
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	value, err := rt.Eval(`JSON.stringify({process: typeof process, results: spawnResults})`)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Process string   `json:"process"`
		Results []string `json:"results"`
	}
	if err := json.Unmarshal([]byte(value.String()), &got); err != nil {
		t.Fatalf("decode spawn results %q: %v", value.String(), err)
	}
	want := []string{
		filepath.Clean(work) + "|selected|default|unset",
		filepath.Clean(work) + "|override|native|unset",
		filepath.Clean(work) + "|selected|unset|unset",
	}
	if got.Process != "undefined" || strings.Join(got.Results, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Profile spawn process/results = %q/%#v, want undefined/%#v", got.Process, got.Results, want)
	}
	if len(allowed) != 3 || allowed[0] != "probe" || allowed[1] != "probe" || allowed[2] != defaultCommand {
		t.Fatalf("Allow requests = %#v", allowed)
	}
}

func TestProfileSpawnHonorsTimeoutForChildLifetime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	slowCommand := filepath.Join(root, "slow")
	if err := os.WriteFile(slowCommand, []byte("#!/bin/sh\n/bin/sleep 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := &commandenv.Config{
		Cwd:     work,
		Env:     map[string]string{},
		Timeout: 300 * time.Millisecond,
		Allow:   func(string, []string) error { return nil },
	}
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	t.Cleanup(func() { _ = rt.Close() })
	if err := stdlib.Install(rt, stdlib.Config{Loop: loop}); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSelected(rt, Options{Cwd: work, Command: config}, Modules{Commands: true}); err != nil {
		t.Fatal(err)
	}
	commandJSON, _ := json.Marshal(slowCommand)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	script := fmt.Sprintf(`
		import {spawn} from "noe:process";
		(async () => {
			const child = await spawn(%s, [], {stdin:"ignore", stdout:"ignore", stderr:"ignore"});
			globalThis.timeoutResult = await child.exited;
			await child.close();
		})();
	`, commandJSON)
	if _, err := rt.EvalModuleContext(ctx, "profile-spawn-timeout.mjs", script); err != nil {
		t.Fatal(err)
	}
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	value, err := rt.Eval(`JSON.stringify(timeoutResult)`)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Code   *int   `json:"code"`
		Signal string `json:"signal"`
	}
	if err := json.Unmarshal([]byte(value.String()), &result); err != nil {
		t.Fatalf("decode timeout result %q: %v", value.String(), err)
	}
	if result.Signal == "" {
		t.Fatalf("timed out command was not signaled: %#v", result)
	}
}

func TestSpawnUsesSelectedSupervisor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses Job Objects instead of a helper executable")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		profile bool
	}{
		{name: "legacy options"},
		{name: "profile command config", profile: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			supervisor := filepath.Join(cwd, "missing-supervisor")
			options := Options{Cwd: cwd, Supervisor: supervisor}
			if tc.profile {
				options.Command = &commandenv.Config{
					Cwd:        cwd,
					Env:        map[string]string{},
					Allow:      func(string, []string) error { return nil },
					Supervisor: supervisor,
				}
			}
			rt := quickjs.New()
			loop := stdlib.NewLoop(rt)
			rt.OnClose(loop.Close)
			t.Cleanup(func() { _ = rt.Close() })
			if err := stdlib.Install(rt, stdlib.Config{Loop: loop}); err != nil {
				t.Fatal(err)
			}
			if _, err := InstallSelected(rt, options, Modules{Commands: true}); err != nil {
				t.Fatal(err)
			}
			commandJSON, _ := json.Marshal(executable)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			script := fmt.Sprintf(`
				import {spawn} from "noe:process";
				(async () => {
					try {
						await spawn(%s, [], {stdin:"ignore", stdout:"ignore", stderr:"ignore"});
						globalThis.supervisorMessage = "spawn unexpectedly succeeded";
					} catch (error) {
						globalThis.supervisorMessage = error.message;
					}
				})();
			`, commandJSON)
			if _, err := rt.EvalModuleContext(ctx, "native-supervisor.mjs", script); err != nil {
				t.Fatal(err)
			}
			if err := loop.Run(ctx); err != nil {
				t.Fatal(err)
			}
			value, err := rt.Eval(`supervisorMessage`)
			if err != nil {
				t.Fatal(err)
			}
			message := value.String()
			if !strings.Contains(message, "start process supervisor") || !strings.Contains(message, supervisor) {
				t.Fatalf("spawn error = %q, want explicit supervisor %q", message, supervisor)
			}
		})
	}
}

func writeNativeCommandFixture(t *testing.T, dir, value string) string {
	t.Helper()
	path := filepath.Join(dir, "probe")
	contents := "#!/bin/sh\nprintf '%s|%s|%s|%s\\n' \"$PWD\" \"" + value + "\" \"${MARK-unset}\" \"${NOE_NATIVE_HOST_SECRET-unset}\"\n"
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
