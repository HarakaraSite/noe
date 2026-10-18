//go:build e2e

package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileMutationLifecycleThroughCLI(t *testing.T) {
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "existing.txt"), "old")
	source := `import {mkdir,writeFile,readFile,rename,unlink,rmdir} from "node:fs/promises";
await mkdir("親/子", {recursive:true});
await mkdir(process.cwd() + "/親/子", {recursive:true});
await writeFile("親/子/元.txt", "雪\n");
await rename("親/子/元.txt", "existing.txt");
if (await readFile("existing.txt", "utf8") !== "雪\n") throw new Error("rename content");
await rename("親/子", "親/移動先");
await unlink(process.cwd() + "/existing.txt");
await rmdir("親/移動先");
await rmdir("親");
mkdir("unawaited/nested", {recursive:true});
console.log("finished");`
	got := runCLI(t, work, nil, "eval", source)
	requireExit(t, got, 0)
	if got.stdout != "finished\n" || got.stderr != "" {
		t.Fatalf("lifecycle result: %+v", got)
	}
	for _, path := range []string{"existing.txt", "親"} {
		if _, err := os.Lstat(filepath.Join(work, path)); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed, err=%v", path, err)
		}
	}
	if info, err := os.Stat(filepath.Join(work, "unawaited", "nested")); err != nil || !info.IsDir() {
		t.Fatalf("unawaited mkdir did not finish: info=%v err=%v", info, err)
	}
}

func TestFileMutationLinksErrorsAndREPLRecovery(t *testing.T) {
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "target.txt"), "kept")
	writeFixture(t, filepath.Join(work, "not-empty", "child.txt"), "kept")
	if err := os.Symlink("target.txt", filepath.Join(work, "link")); err != nil {
		t.Fatal(err)
	}
	got := runCLI(t, work, nil, "eval", `import {rename,unlink} from "node:fs/promises"; await rename("link", "moved-link"); await unlink("moved-link");`)
	requireExit(t, got, 0)
	if got.stdout != "" || got.stderr != "" {
		t.Fatalf("link operations: %+v", got)
	}
	if content, err := os.ReadFile(filepath.Join(work, "target.txt")); err != nil || string(content) != "kept" {
		t.Fatalf("link target was affected: content=%q err=%v", content, err)
	}
	if _, err := os.Lstat(filepath.Join(work, "moved-link")); !os.IsNotExist(err) {
		t.Fatalf("link not removed: %v", err)
	}
	for _, source := range []string{
		`import {mkdir} from "node:fs/promises"; await mkdir("target.txt/child", {recursive:true})`,
		`import {rename} from "node:fs/promises"; await rename("missing", "renamed")`,
		`import {unlink} from "node:fs/promises"; await unlink("missing")`,
		`import {rmdir} from "node:fs/promises"; await rmdir("not-empty")`,
	} {
		failed := runCLI(t, work, nil, "eval", source)
		requireExit(t, failed, 1)
		if failed.stdout != "" || failed.stderr == "" {
			t.Fatalf("failure %q: %+v", source, failed)
		}
	}
	repl := runREPLInput(t, work, map[string]string{"NOE_REPL_HISTORY": ""}, strings.Join([]string{
		`const fs = await import("node:fs/promises")`,
		`await fs.mkdir("repl/nested", {recursive:true})`,
		`await fs.rename("repl/nested", "repl/moved")`,
		`await fs.rmdir("repl/moved")`,
		`await fs.rmdir("not-empty")`,
		`console.log("RECOVERED", 40 + 2)`,
	}, "\n")+"\n")
	requireExit(t, repl, 0)
	if !strings.Contains(repl.stdout, "RECOVERED 42") || !strings.Contains(repl.stderr, "not-empty") {
		t.Fatalf("REPL failed to recover: %+v", repl)
	}
	if content, err := os.ReadFile(filepath.Join(work, "not-empty", "child.txt")); err != nil || string(content) != "kept" {
		t.Fatalf("nonempty directory child was affected: content=%q err=%v", content, err)
	}
}

func TestProcessJSONExampleSavesAndArchives(t *testing.T) {
	work := t.TempDir()
	input := filepath.Join(work, "input")
	output := filepath.Join(work, "output", "nested")
	inputs := map[string]string{"first.json": "[100,200,300]\n", "日本語.json": "[50,150]\n"}
	for name, content := range inputs {
		writeFixture(t, filepath.Join(input, name), content)
	}
	writeFixture(t, filepath.Join(input, "notes.txt"), "ignored")
	writeFixture(t, filepath.Join(input, "subdirectory", "ignored.json"), "[999]")
	writeFixture(t, filepath.Join(output, "processed", "first.json"), "old archived content")
	writeFixture(t, filepath.Join(output, "report.json"), "old report")
	if err := os.Symlink("first.json", filepath.Join(input, "linked.json")); err != nil {
		t.Fatal(err)
	}
	example := filepath.Join(repoRootPath, "examples", "json", "process-json.js")
	got := runCLI(t, work, nil, "run", example, "input", "output/nested")
	requireExit(t, got, 0)
	if got.stderr != "" {
		t.Fatalf("process example diagnostic: %q", got.stderr)
	}
	var report struct {
		Files []struct {
			Name  string `json:"name"`
			Bytes int    `json:"bytes"`
			Count int    `json:"count"`
			Total int    `json:"total"`
		} `json:"files"`
		Count   int `json:"count"`
		Total   int `json:"total"`
		Average int `json:"average"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &report); err != nil {
		t.Fatalf("invalid stdout JSON %q: %v", got.stdout, err)
	}
	if report.Count != 5 || report.Total != 800 || report.Average != 160 || len(report.Files) != 2 {
		t.Fatalf("wrong report: %+v", report)
	}
	for i, name := range []string{"first.json", "日本語.json"} {
		if report.Files[i].Name != name || report.Files[i].Bytes != len(inputs[name]) {
			t.Fatalf("wrong file metadata: %+v", report.Files[i])
		}
		if _, err := os.Stat(filepath.Join(input, name)); !os.IsNotExist(err) {
			t.Fatalf("input %s not moved: %v", name, err)
		}
		if content, err := os.ReadFile(filepath.Join(output, "processed", name)); err != nil || string(content) != inputs[name] {
			t.Fatalf("archive %s: content=%q err=%v", name, content, err)
		}
	}
	saved, err := os.ReadFile(filepath.Join(output, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var savedReport, printedReport any
	if err := json.Unmarshal(saved, &savedReport); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got.stdout), &printedReport); err != nil {
		t.Fatal(err)
	}
	savedJSON, _ := json.Marshal(savedReport)
	printedJSON, _ := json.Marshal(printedReport)
	if string(savedJSON) != string(printedJSON) {
		t.Fatalf("saved report differs: saved=%s printed=%s", saved, got.stdout)
	}
	for _, path := range []string{"report.json.tmp", ".processing"} {
		if _, err := os.Lstat(filepath.Join(output, path)); !os.IsNotExist(err) {
			t.Fatalf("scratch path %s remains: %v", path, err)
		}
	}
	for _, path := range []string{"notes.txt", "subdirectory/ignored.json", "linked.json"} {
		if _, err := os.Lstat(filepath.Join(input, path)); err != nil {
			t.Fatalf("skipped input %s affected: %v", path, err)
		}
	}
}

func TestProcessJSONExampleEmptyUsageAndInvalidData(t *testing.T) {
	example := filepath.Join(repoRootPath, "examples", "json", "process-json.js")
	work := t.TempDir()
	usage := runCLI(t, work, nil, "run", example)
	requireExit(t, usage, 2)
	if usage.stdout != "" || !strings.Contains(usage.stderr, "Usage:") {
		t.Fatalf("usage result: %+v", usage)
	}
	if err := os.Mkdir(filepath.Join(work, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	empty := runCLI(t, work, nil, "run", example, "empty", "empty-output")
	requireExit(t, empty, 0)
	if empty.stdout != "{\"files\":[],\"count\":0,\"total\":0,\"average\":null}\n" || empty.stderr != "" {
		t.Fatalf("empty result: %+v", empty)
	}
	for _, content := range []string{"{", `[1,"two"]`} {
		t.Run(content, func(t *testing.T) {
			dir := t.TempDir()
			writeFixture(t, filepath.Join(dir, "input", "valid.json"), "[1,2]")
			writeFixture(t, filepath.Join(dir, "input", "invalid.json"), content)
			failed := runCLI(t, dir, nil, "run", example, "input", "output")
			requireExit(t, failed, 1)
			if failed.stdout != "" || failed.stderr == "" {
				t.Fatalf("invalid data: %+v", failed)
			}
			for _, name := range []string{"valid.json", "invalid.json"} {
				if _, err := os.Stat(filepath.Join(dir, "input", name)); err != nil {
					t.Fatalf("parse failure moved %s: %v", name, err)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "output")); !os.IsNotExist(err) {
				t.Fatalf("parse failure wrote output: %v", err)
			}
		})
	}
}

func TestProcessJSONExamplePartialFailureKeepsCompletedChanges(t *testing.T) {
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "input", "first.json"), "[1]")
	writeFixture(t, filepath.Join(work, "input", "second.json"), "[2]")
	// A directory at the second archive destination makes the second rename
	// fail after the report and first input have already been saved/moved.
	writeFixture(t, filepath.Join(work, "output", "processed", "second.json", "child.txt"), "kept")
	example := filepath.Join(repoRootPath, "examples", "json", "process-json.js")
	got := runCLI(t, work, nil, "run", example, "input", "output")
	requireExit(t, got, 1)
	if got.stdout != "" || !strings.Contains(got.stderr, "second.json") {
		t.Fatalf("partial failure result: %+v", got)
	}
	for path, expected := range map[string]string{
		"output/processed/first.json":            "[1]",
		"input/second.json":                      "[2]",
		"output/processed/second.json/child.txt": "kept",
		"output/.processing":                     "NOE processing\n",
	} {
		if content, err := os.ReadFile(filepath.Join(work, path)); err != nil || string(content) != expected {
			t.Fatalf("partial failure %s: content=%q err=%v", path, content, err)
		}
	}
	if _, err := os.Stat(filepath.Join(work, "input", "first.json")); !os.IsNotExist(err) {
		t.Fatalf("completed first move was rolled back: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(work, "output", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Count int `json:"count"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(content, &report); err != nil || report.Count != 2 || report.Total != 3 {
		t.Fatalf("completed report not preserved: content=%s report=%+v err=%v", content, report, err)
	}
}
