package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileMutationAPIs(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSession(Options{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	deepPath := filepath.Join("日本語", "入れ子", "出力")
	assertMutationUndefined(t, s, `await noe.mkdir(`+jsString(t, deepPath)+`)`)
	if info, err := os.Stat(filepath.Join(dir, deepPath)); err != nil || !info.IsDir() {
		t.Fatalf("mkdir nested path: info=%v err=%v", info, err)
	}
	assertMutationUndefined(t, s, `await noe.mkdir(`+jsString(t, filepath.Join(dir, deepPath))+`)`)

	if err := os.WriteFile(filepath.Join(dir, "mkdir-file"), []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	assertMutationRejects(t, s, `await noe.mkdir("mkdir-file")`)

	if err := os.WriteFile(filepath.Join(dir, "before.txt"), []byte("new contents"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "after.txt"), []byte("old contents"), 0600); err != nil {
		t.Fatal(err)
	}
	assertMutationUndefined(t, s, `await noe.rename("before.txt", "after.txt")`)
	if data, err := os.ReadFile(filepath.Join(dir, "after.txt")); err != nil || string(data) != "new contents" {
		t.Fatalf("renamed file contents=%q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "before.txt")); !os.IsNotExist(err) {
		t.Fatalf("rename left source path: %v", err)
	}

	if err := os.Mkdir(filepath.Join(dir, "dir-before"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dir-before", "inside.txt"), []byte("inside"), 0600); err != nil {
		t.Fatal(err)
	}
	assertMutationUndefined(t, s, `await noe.rename("dir-before", "dir-after")`)
	if data, err := os.ReadFile(filepath.Join(dir, "dir-after", "inside.txt")); err != nil || string(data) != "inside" {
		t.Fatalf("renamed directory contents=%q err=%v", data, err)
	}

	target := filepath.Join(dir, "link-target.txt")
	if err := os.WriteFile(target, []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(target), filepath.Join(dir, "link-before")); err != nil {
		t.Fatal(err)
	}
	assertMutationUndefined(t, s, `await noe.rename("link-before", "link-after")`)
	linkInfo, err := os.Lstat(filepath.Join(dir, "link-after"))
	if err != nil || linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("renamed path is not a symlink: info=%v err=%v", linkInfo, err)
	}
	linkTarget, err := os.Readlink(filepath.Join(dir, "link-after"))
	if err != nil || linkTarget != filepath.Base(target) {
		t.Fatalf("renamed link target=%q err=%v", linkTarget, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "target" {
		t.Fatalf("rename changed link target: data=%q err=%v", data, err)
	}

	if err := os.WriteFile(filepath.Join(dir, deepPath, "源.txt"), []byte("日本語"), 0600); err != nil {
		t.Fatal(err)
	}
	absoluteSource := filepath.Join(dir, deepPath, "源.txt")
	absoluteTarget := filepath.Join(dir, deepPath, "移動先.txt")
	assertMutationUndefined(t, s, `await noe.rename(`+jsString(t, absoluteSource)+`, `+jsString(t, absoluteTarget)+`)`)
	if data, err := os.ReadFile(absoluteTarget); err != nil || string(data) != "日本語" {
		t.Fatalf("absolute Japanese rename contents=%q err=%v", data, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "remove-file"), []byte("remove"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "remove-empty"), 0755); err != nil {
		t.Fatal(err)
	}
	assertMutationUndefined(t, s, `await noe.remove("remove-file")`)
	assertMutationUndefined(t, s, `await noe.remove("remove-empty")`)
	if _, err := os.Lstat(filepath.Join(dir, "remove-file")); !os.IsNotExist(err) {
		t.Fatalf("remove left file: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "remove-empty")); !os.IsNotExist(err) {
		t.Fatalf("remove left empty directory: %v", err)
	}

	if err := os.Symlink(filepath.Base(target), filepath.Join(dir, "remove-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(dir, "remove-dangling")); err != nil {
		t.Fatal(err)
	}
	assertMutationUndefined(t, s, `await noe.remove("remove-link")`)
	assertMutationUndefined(t, s, `await noe.remove("remove-dangling")`)
	if _, err := os.Lstat(filepath.Join(dir, "remove-link")); !os.IsNotExist(err) {
		t.Fatalf("remove left symlink: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "remove-dangling")); !os.IsNotExist(err) {
		t.Fatalf("remove left dangling symlink: %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "target" {
		t.Fatalf("remove changed symlink target: data=%q err=%v", data, err)
	}

	if err := os.Mkdir(filepath.Join(dir, "remove-nonempty"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "remove-nonempty", "inside"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	assertMutationRejects(t, s, `await noe.remove("remove-nonempty")`)
	assertMutationRejects(t, s, `await noe.remove("missing-path")`)
	assertMutationRejects(t, s, `await noe.rename("missing-source", "rename-target")`)
	assertMutationRejects(t, s, `await noe.rename("after.txt", "missing-parent/target")`)
}

func TestFileMutationErrorsRecoverInSession(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain-file"), []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(Options{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, input := range []string{
		`await noe.mkdir("plain-file/child")`,
		`await noe.rename("plain-file", "missing-parent/target")`,
		`await noe.remove("missing-path")`,
	} {
		assertMutationRejects(t, s, input)
		got := s.Eval(context.Background(), `40 + 2`)
		if got.Exit || got.Err != nil || got.Output != "42" {
			t.Fatalf("session did not recover after %q: %+v", input, got)
		}
	}
}

func TestFileMutationUnawaitedWorkCompletesBeforeExecuteReturns(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rename-source"), []byte("renamed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "remove-source"), []byte("removed"), 0600); err != nil {
		t.Fatal(err)
	}
	source := `noe.mkdir("created/nested"); noe.rename("rename-source", "rename-target"); noe.remove("remove-source");`
	code, err := Execute(context.Background(), Source{Name: filepath.Join(dir, "main.js"), Code: source}, Options{Cwd: dir})
	if code != 0 || err != nil {
		t.Fatalf("Execute code=%d err=%v", code, err)
	}
	if info, err := os.Stat(filepath.Join(dir, "created", "nested")); err != nil || !info.IsDir() {
		t.Fatalf("unawaited mkdir: info=%v err=%v", info, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "rename-target")); err != nil || string(data) != "renamed" {
		t.Fatalf("unawaited rename: data=%q err=%v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "remove-source")); !os.IsNotExist(err) {
		t.Fatalf("unawaited remove left source: %v", err)
	}
}

func TestFileMutationCanceledContextPreventsOperations(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	removePath := filepath.Join(dir, "remove")
	if err := os.WriteFile(source, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(removePath, []byte("remove"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := mkdirContext(ctx, filepath.Join(dir, "not-created", "nested")); !errors.Is(err, context.Canceled) {
		t.Fatalf("mkdir with canceled context err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "not-created")); !os.IsNotExist(err) {
		t.Fatalf("canceled mkdir created path: %v", err)
	}
	if err := renameContext(ctx, source, filepath.Join(dir, "renamed")); !errors.Is(err, context.Canceled) {
		t.Fatalf("rename with canceled context err=%v", err)
	}
	if data, err := os.ReadFile(source); err != nil || string(data) != "source" {
		t.Fatalf("canceled rename changed source: data=%q err=%v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "renamed")); !os.IsNotExist(err) {
		t.Fatalf("canceled rename created destination: %v", err)
	}
	if err := removeContext(ctx, removePath); !errors.Is(err, context.Canceled) {
		t.Fatalf("remove with canceled context err=%v", err)
	}
	if data, err := os.ReadFile(removePath); err != nil || string(data) != "remove" {
		t.Fatalf("canceled remove changed file: data=%q err=%v", data, err)
	}
}

func assertMutationUndefined(t *testing.T, s *Session, input string) {
	t.Helper()
	result := s.Eval(context.Background(), input+` === undefined`)
	if result.Exit || result.Err != nil || result.Output != "true" {
		t.Fatalf("input=%q result=%+v, want Promise<void>", input, result)
	}
}

func assertMutationRejects(t *testing.T, s *Session, input string) {
	t.Helper()
	result := s.Eval(context.Background(), input)
	if result.Exit || result.Err == nil {
		t.Fatalf("input=%q result=%+v, want rejected Promise", input, result)
	}
	if diagnostic := Diagnostic(result.Err); strings.TrimSpace(diagnostic) == "" {
		t.Fatalf("input=%q rejection had empty diagnostic", input)
	}
}
