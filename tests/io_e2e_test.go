//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func runCLIWithInput(t *testing.T, dir string, stdin string, args ...string) cliResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliPath, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("noe %v exceeded the 10s process limit: %v\nstdout:\n%s\nstderr:\n%s", args, ctx.Err(), stdout.String(), stderr.String())
	}
	result := cliResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run noe %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, result.stdout, result.stderr)
	}
	result.exitCode = exitErr.ExitCode()
	return result
}

func TestReadStdinRunAndEvalPreserveInputAndSharePromise(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "unicode with trailing newline", input: "{\"message\":\"日本語🌱\"}\nsecond line\n"},
		{name: "final text without newline", input: "last line: 雪だるま☃"},
		{name: "empty", input: ""},
	}
	source := `const first = noe.readStdin();
const samePromise = first === noe.readStdin();
const [value, again] = await Promise.all([first, noe.readStdin()]);
console.log(JSON.stringify({ samePromise, value, again }));`

	for _, mode := range []string{"run", "eval"} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				work := t.TempDir()
				args := []string{"eval", source}
				if mode == "run" {
					script := filepath.Join(work, "stdin.js")
					writeFixture(t, script, source)
					args = []string{"run", script}
				}
				got := runCLIWithInput(t, work, tc.input, args...)
				requireExit(t, got, 0)
				wantJSON, err := json.Marshal(struct {
					SamePromise bool   `json:"samePromise"`
					Value       string `json:"value"`
					Again       string `json:"again"`
				}{SamePromise: true, Value: tc.input, Again: tc.input})
				if err != nil {
					t.Fatal(err)
				}
				if got.stdout != string(wantJSON)+"\n" || got.stderr != "" {
					t.Fatalf("run/eval stdin result: stdout=%q stderr=%q, want stdout=%q and empty stderr", got.stdout, got.stderr, string(wantJSON)+"\n")
				}
			})
		}
	}
}

func TestReadStdinWaitsForUnawaitedRead(t *testing.T) {
	work := t.TempDir()
	script := filepath.Join(work, "unawaited.js")
	writeFixture(t, script, `noe.readStdin().then(value => console.log(JSON.stringify(value)));
console.log("script returned");`)

	got := runCLIWithInput(t, work, "payload 日本語\n", "run", script)
	requireExit(t, got, 0)
	if got.stdout != "script returned\n\"payload 日本語\\n\"\n" || got.stderr != "" {
		t.Fatalf("unawaited stdin result: stdout=%q stderr=%q", got.stdout, got.stderr)
	}
}

func TestReadStdinCancellationAndExplicitExitWithOpenPipe(t *testing.T) {
	t.Run("SIGINT cancels pending read", func(t *testing.T) {
		work := t.TempDir()
		script := filepath.Join(work, "cancel.js")
		writeFixture(t, script, `const pending = noe.readStdin();
console.log("STDIN_READ_PENDING");
await pending;
console.log("AFTER_READ");`)
		process := startCLIWithOpenStdin(t, work, "run", script)
		if !process.stdout.waitFor("STDIN_READ_PENDING\n", 3*time.Second) {
			t.Fatalf("CLI did not start its stdin read: stdout=%q stderr=%q", process.stdout.String(), process.stderr.String())
		}
		if err := process.cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("send SIGINT to pending run: %v", err)
		}
		got := process.wait(t, 3*time.Second)
		requireExit(t, got, 1)
		if got.stderr == "" || strings.Contains(got.stdout, "AFTER_READ") {
			t.Fatalf("cancelled stdin read: stdout=%q stderr=%q", got.stdout, got.stderr)
		}
	})

	t.Run("noe.exit terminates pending read", func(t *testing.T) {
		work := t.TempDir()
		script := filepath.Join(work, "exit.js")
		writeFixture(t, script, `const pending = noe.readStdin();
console.log("STDIN_READ_PENDING");
noe.exit(7);
console.log("AFTER_EXIT");`)
		process := startCLIWithOpenStdin(t, work, "run", script)
		if !process.stdout.waitFor("STDIN_READ_PENDING\n", 3*time.Second) {
			t.Fatalf("CLI did not start its stdin read: stdout=%q stderr=%q", process.stdout.String(), process.stderr.String())
		}
		got := process.wait(t, 3*time.Second)
		requireExit(t, got, 7)
		if got.stdout != "STDIN_READ_PENDING\n" || got.stderr != "" {
			t.Fatalf("explicit exit with open stdin: stdout=%q stderr=%q", got.stdout, got.stderr)
		}
	})
}

type synchronizedCapture struct {
	mu      sync.Mutex
	data    bytes.Buffer
	changed chan struct{}
}

func newSynchronizedCapture() *synchronizedCapture {
	return &synchronizedCapture{changed: make(chan struct{}, 1)}
}

func (capture *synchronizedCapture) Write(data []byte) (int, error) {
	capture.mu.Lock()
	n, err := capture.data.Write(data)
	capture.mu.Unlock()
	select {
	case capture.changed <- struct{}{}:
	default:
	}
	return n, err
}

func (capture *synchronizedCapture) String() string {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.data.String()
}

func (capture *synchronizedCapture) waitFor(text string, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		if strings.Contains(capture.String(), text) {
			return true
		}
		select {
		case <-capture.changed:
		case <-timer.C:
			return false
		}
	}
}

type openStdinCLI struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *synchronizedCapture
	stderr *synchronizedCapture
	done   chan struct{}
	err    error
}

func startCLIWithOpenStdin(t *testing.T, dir string, args ...string) *openStdinCLI {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	cmd := exec.CommandContext(ctx, cliPath, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	process := &openStdinCLI{
		cmd:    cmd,
		stdin:  stdin,
		stdout: newSynchronizedCapture(),
		stderr: newSynchronizedCapture(),
		done:   make(chan struct{}),
	}
	cmd.Stdout = process.stdout
	cmd.Stderr = process.stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		t.Fatal(err)
	}
	go func() {
		process.err = cmd.Wait()
		close(process.done)
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		select {
		case <-process.done:
		case <-time.After(time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-process.done
		}
		cancel()
	})
	return process
}

func (process *openStdinCLI) wait(t *testing.T, timeout time.Duration) cliResult {
	t.Helper()
	select {
	case <-process.done:
	case <-time.After(timeout):
		t.Fatalf("noe did not exit within %s with stdin still open\nstdout:\n%s\nstderr:\n%s", timeout, process.stdout.String(), process.stderr.String())
	}
	result := cliResult{stdout: process.stdout.String(), stderr: process.stderr.String()}
	if process.err == nil {
		return result
	}
	var exitErr *exec.ExitError
	if !errors.As(process.err, &exitErr) {
		t.Fatalf("run noe with open stdin: %v\nstdout:\n%s\nstderr:\n%s", process.err, result.stdout, result.stderr)
	}
	result.exitCode = exitErr.ExitCode()
	return result
}

func TestSummarizeStdinExampleAndPipeline(t *testing.T) {
	work := t.TempDir()
	example := filepath.Join(repoRootPath, "examples", "summarize-stdin.js")
	got := runCLIWithInput(t, work, "[100,200,300]\n", "run", example)
	requireExit(t, got, 0)
	if got.stdout != "{\"count\":3,\"total\":600,\"average\":200}\n" || got.stderr != "" {
		t.Fatalf("summarize example: stdout=%q stderr=%q", got.stdout, got.stderr)
	}

	verify := `const value = JSON.parse(await noe.readStdin());
if (value.count !== 3 || value.total !== 600 || value.average !== 200) throw new Error("pipeline value mismatch");
console.log("pipeline verified");`
	piped := runCLIWithInput(t, work, got.stdout, "eval", verify)
	requireExit(t, piped, 0)
	if piped.stdout != "pipeline verified\n" || piped.stderr != "" {
		t.Fatalf("summary stdout pipeline: stdout=%q stderr=%q", piped.stdout, piped.stderr)
	}

	empty := runCLIWithInput(t, work, "[]", "run", example)
	requireExit(t, empty, 0)
	if empty.stdout != "{\"count\":0,\"total\":0,\"average\":null}\n" || empty.stderr != "" {
		t.Fatalf("empty summary: stdout=%q stderr=%q", empty.stdout, empty.stderr)
	}

	for _, input := range []string{"not-json", `[1,"two"]`} {
		invalid := runCLIWithInput(t, work, input, "run", example)
		requireExit(t, invalid, 1)
		if invalid.stdout != "" || invalid.stderr == "" {
			t.Errorf("invalid summarize input %q: stdout=%q stderr=%q", input, invalid.stdout, invalid.stderr)
		}
	}
}

func TestCollectJSONExampleAggregatesSortedFilesAndSkipsOtherEntries(t *testing.T) {
	work := t.TempDir()
	example := filepath.Join(repoRootPath, "examples", "collect-json.js")
	fixtureDir := filepath.Join(repoRootPath, "examples", "data", "numbers")
	firstInfo, err := os.Stat(filepath.Join(fixtureDir, "first.json"))
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(filepath.Join(fixtureDir, "second.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := runCLI(t, repoRootPath, nil, "run", example, filepath.Join("examples", "data", "numbers"))
	requireExit(t, got, 0)
	want := fmt.Sprintf(`{"files":[{"name":"first.json","bytes":%d,"count":3,"total":600},{"name":"second.json","bytes":%d,"count":2,"total":200}],"count":5,"total":800,"average":160}`+"\n", firstInfo.Size(), secondInfo.Size())
	if got.stdout != want || got.stderr != "" {
		t.Fatalf("collect example fixtures: stdout=%q want=%q stderr=%q", got.stdout, want, got.stderr)
	}

	emptyDir := filepath.Join(work, "empty")
	if err := os.Mkdir(emptyDir, 0700); err != nil {
		t.Fatal(err)
	}
	empty := runCLI(t, work, nil, "run", example, emptyDir)
	requireExit(t, empty, 0)
	if empty.stdout != `{"files":[],"count":0,"total":0,"average":null}`+"\n" || empty.stderr != "" {
		t.Fatalf("empty collect: stdout=%q stderr=%q", empty.stdout, empty.stderr)
	}

	inputsDir := filepath.Join(work, "inputs")
	if err := os.MkdirAll(filepath.Join(inputsDir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(inputsDir, "first.json"), "[2,3]")
	writeFixture(t, filepath.Join(inputsDir, "second.json"), "[4]")
	writeFixture(t, filepath.Join(inputsDir, "notes.txt"), "ignored")
	writeFixture(t, filepath.Join(inputsDir, "nested", "nested.json"), "[900]")
	if err := os.Symlink(filepath.Join(inputsDir, "first.json"), filepath.Join(inputsDir, "linked.json")); err != nil {
		t.Fatal(err)
	}
	skipped := runCLI(t, work, nil, "run", example, inputsDir)
	requireExit(t, skipped, 0)
	firstBytes, err := os.Stat(filepath.Join(inputsDir, "first.json"))
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.Stat(filepath.Join(inputsDir, "second.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantSkipped := fmt.Sprintf(`{"files":[{"name":"first.json","bytes":%d,"count":2,"total":5},{"name":"second.json","bytes":%d,"count":1,"total":4}],"count":3,"total":9,"average":3}`+"\n", firstBytes.Size(), secondBytes.Size())
	if skipped.stdout != wantSkipped || skipped.stderr != "" {
		t.Fatalf("collect skipped entries: stdout=%q want=%q stderr=%q", skipped.stdout, wantSkipped, skipped.stderr)
	}
}

func TestCollectJSONExampleReportsUsageAndInvalidFiles(t *testing.T) {
	work := t.TempDir()
	example := filepath.Join(repoRootPath, "examples", "collect-json.js")
	missingArg := runCLI(t, work, nil, "run", example)
	requireExit(t, missingArg, 2)
	if missingArg.stdout != "" || !strings.Contains(missingArg.stderr, "Usage:") {
		t.Fatalf("collect missing argument: stdout=%q stderr=%q", missingArg.stdout, missingArg.stderr)
	}

	for _, tc := range []struct {
		name    string
		content string
	}{
		{name: "invalid JSON", content: "{"},
		{name: "non-numeric data", content: `[1,"two"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixture(t, filepath.Join(dir, "bad.json"), tc.content)
			got := runCLI(t, work, nil, "run", example, dir)
			requireExit(t, got, 1)
			if got.stdout != "" || got.stderr == "" {
				t.Fatalf("invalid collect input: stdout=%q stderr=%q", got.stdout, got.stderr)
			}
		})
	}
}

type e2eDirEntry struct {
	Name        string `json:"name"`
	IsFile      bool   `json:"isFile"`
	IsDirectory bool   `json:"isDirectory"`
	IsSymlink   bool   `json:"isSymlink"`
}

type e2eFileStat struct {
	Size        int64  `json:"size"`
	ModifiedAt  string `json:"modifiedAt"`
	IsFile      bool   `json:"isFile"`
	IsDirectory bool   `json:"isDirectory"`
}

func TestReadDirAndStatMetadataThroughCLI(t *testing.T) {
	work := t.TempDir()
	fileContent := "値🌟\n"
	writeFixture(t, filepath.Join(work, "日本語.txt"), fileContent)
	writeFixture(t, filepath.Join(work, "folder", "nested.txt"), "nested")
	if err := os.Mkdir(filepath.Join(work, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(work, "日本語.txt"), filepath.Join(work, "linked-file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(work, "folder"), filepath.Join(work, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(work, "metadata.js")
	writeFixture(t, script, `const entries = await noe.readDir(".");
const nested = await noe.readDir("folder");
const empty = await noe.readDir("empty");
const file = await noe.stat("日本語.txt");
const absolute = await noe.stat(noe.cwd() + "/日本語.txt");
const fileLink = await noe.stat("linked-file");
const dirLink = await noe.stat("linked-dir");
console.log(JSON.stringify({ entries, nested, empty, file, absolute, fileLink, dirLink }));`)

	got := runCLI(t, work, nil, "run", script)
	requireExit(t, got, 0)
	if got.stderr != "" {
		t.Fatalf("metadata diagnostic: %q", got.stderr)
	}
	var result struct {
		Entries  []e2eDirEntry `json:"entries"`
		Nested   []e2eDirEntry `json:"nested"`
		Empty    []e2eDirEntry `json:"empty"`
		File     e2eFileStat   `json:"file"`
		Absolute e2eFileStat   `json:"absolute"`
		FileLink e2eFileStat   `json:"fileLink"`
		DirLink  e2eFileStat   `json:"dirLink"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &result); err != nil {
		t.Fatalf("decode metadata JSON %q: %v", got.stdout, err)
	}
	wantNames := []string{"empty", "folder", "linked-dir", "linked-file", "metadata.js", "日本語.txt"}
	if len(result.Entries) != len(wantNames) {
		t.Fatalf("readDir entries = %#v, want names %q", result.Entries, wantNames)
	}
	for index, want := range wantNames {
		if result.Entries[index].Name != want {
			t.Fatalf("readDir entry %d = %#v, want name %q in sorted order", index, result.Entries[index], want)
		}
	}
	for _, entry := range result.Entries {
		switch entry.Name {
		case "empty", "folder":
			if entry.IsFile || !entry.IsDirectory || entry.IsSymlink {
				t.Errorf("directory entry %q = %#v", entry.Name, entry)
			}
		case "linked-dir", "linked-file":
			if entry.IsFile || entry.IsDirectory || !entry.IsSymlink {
				t.Errorf("symlink entry %q followed its target: %#v", entry.Name, entry)
			}
		default:
			if !entry.IsFile || entry.IsDirectory || entry.IsSymlink {
				t.Errorf("regular file entry %q = %#v", entry.Name, entry)
			}
		}
	}
	if len(result.Nested) != 1 || result.Nested[0] != (e2eDirEntry{Name: "nested.txt", IsFile: true}) {
		t.Fatalf("readDir folder = %#v, want only its direct file", result.Nested)
	}
	if len(result.Empty) != 0 {
		t.Fatalf("readDir empty = %#v, want an empty array", result.Empty)
	}

	wantSize := int64(len([]byte(fileContent)))
	for name, info := range map[string]e2eFileStat{
		"relative file":   result.File,
		"absolute file":   result.Absolute,
		"symlink to file": result.FileLink,
	} {
		if info.Size != wantSize || !info.IsFile || info.IsDirectory {
			t.Errorf("stat %s = %#v, want a regular file of %d bytes", name, info, wantSize)
		}
		parsed, err := time.Parse(time.RFC3339Nano, info.ModifiedAt)
		if err != nil || !strings.HasSuffix(info.ModifiedAt, "Z") {
			t.Errorf("stat %s modifiedAt %q is not UTC RFC3339Nano (parse error %v)", name, info.ModifiedAt, err)
		} else if _, offset := parsed.Zone(); offset != 0 {
			t.Errorf("stat %s modifiedAt %q has nonzero UTC offset", name, info.ModifiedAt)
		}
	}
	if !result.DirLink.IsDirectory || result.DirLink.IsFile {
		t.Errorf("stat symlink to directory = %#v, want target directory metadata", result.DirLink)
	}
}

func TestFilesystemErrorsAndPipedREPLRecovery(t *testing.T) {
	work := t.TempDir()
	for _, tc := range []struct {
		name, source, marker string
	}{
		{name: "readDir", source: `await noe.readDir("missing-directory");`, marker: "missing-directory"},
		{name: "stat", source: `await noe.stat("missing-file");`, marker: "missing-file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := filepath.Join(t.TempDir(), "failure.js")
			writeFixture(t, script, tc.source)
			got := runCLI(t, work, nil, "run", script)
			requireExit(t, got, 1)
			if got.stdout != "" || !strings.Contains(got.stderr, tc.marker) {
				t.Fatalf("%s error result: stdout=%q stderr=%q", tc.name, got.stdout, got.stderr)
			}
		})
	}

	lines := []string{
		`console.log("readStdin", typeof noe.readStdin)`,
		`const entries = await noe.readDir(".")`,
		`console.log("entry count", entries.length)`,
		`await noe.stat("missing-repl-file")`,
		`console.log("REPL_CONTINUED")`,
	}
	repl := runREPLInput(t, work, map[string]string{"NOE_REPL_HISTORY": ""}, strings.Join(lines, "\n")+"\n")
	requireExit(t, repl, 0)
	if !strings.Contains(repl.stdout, "readStdin undefined") || !strings.Contains(repl.stdout, "entry count") || !strings.Contains(repl.stdout, "REPL_CONTINUED") {
		t.Fatalf("piped REPL did not retain JS input or recover: stdout=%q stderr=%q", repl.stdout, repl.stderr)
	}
	if !strings.Contains(repl.stderr, "missing-repl-file") {
		t.Fatalf("piped REPL error was not diagnosed: stdout=%q stderr=%q", repl.stdout, repl.stderr)
	}
}
