package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type filesystemDirEntry struct {
	Name        string `json:"name"`
	IsFile      bool   `json:"isFile"`
	IsDirectory bool   `json:"isDirectory"`
	IsSymlink   bool   `json:"isSymlink"`
}

type filesystemStat struct {
	Size        int64  `json:"size"`
	ModifiedAt  string `json:"modifiedAt"`
	IsFile      bool   `json:"isFile"`
	IsDirectory bool   `json:"isDirectory"`
}

func TestFilesystemAPIs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"docs", "empty"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "nested.txt"), []byte("nested"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "z.txt")
	if err := os.WriteFile(target, []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	modTime := time.Date(2022, time.January, 2, 3, 4, 5, 123456789, time.UTC)
	if err := os.Chtimes(target, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink("z.txt", link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "日本語.txt"), []byte("雪"), 0600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	s, err := NewSession(Options{Cwd: dir, Stdout: &stdout})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	entries := readFilesystemEntries(t, s, &stdout, `await noe.readDir(".")`)
	wantEntries := []filesystemDirEntry{
		{Name: "docs", IsDirectory: true},
		{Name: "empty", IsDirectory: true},
		{Name: "link.txt", IsSymlink: true},
		{Name: "z.txt", IsFile: true},
		{Name: "日本語.txt", IsFile: true},
	}
	if !reflect.DeepEqual(entries, wantEntries) {
		t.Fatalf("relative readDir result = %#v, want %#v", entries, wantEntries)
	}

	empty := readFilesystemEntries(t, s, &stdout, `await noe.readDir("empty")`)
	if len(empty) != 0 {
		t.Fatalf("empty directory result = %#v, want empty array", empty)
	}
	absEntries := readFilesystemEntries(t, s, &stdout, `await noe.readDir(`+jsString(t, dir)+`)`)
	if !reflect.DeepEqual(absEntries, wantEntries) {
		t.Fatalf("absolute readDir result = %#v, want %#v", absEntries, wantEntries)
	}

	linkStat := readFilesystemStat(t, s, &stdout, `await noe.stat("link.txt")`)
	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	wantLinkStat := filesystemStat{
		Size:        int64(len("target")),
		ModifiedAt:  targetInfo.ModTime().UTC().Format(time.RFC3339Nano),
		IsFile:      true,
		IsDirectory: false,
	}
	if linkStat != wantLinkStat {
		t.Fatalf("stat symlink result = %#v, want target metadata %#v", linkStat, wantLinkStat)
	}
	if !strings.HasSuffix(linkStat.ModifiedAt, "Z") {
		t.Fatalf("modifiedAt = %q, want UTC RFC3339 timestamp", linkStat.ModifiedAt)
	}

	unicodePath := filepath.Join(dir, "日本語.txt")
	unicodeStat := readFilesystemStat(t, s, &stdout, `await noe.stat(`+jsString(t, unicodePath)+`)`)
	if unicodeStat.Size != int64(len("雪")) || !unicodeStat.IsFile || unicodeStat.IsDirectory {
		t.Fatalf("stat Japanese absolute path result = %#v", unicodeStat)
	}
	directoryStat := readFilesystemStat(t, s, &stdout, `await noe.stat("docs")`)
	if directoryStat.IsFile || !directoryStat.IsDirectory {
		t.Fatalf("stat directory result = %#v", directoryStat)
	}
}

func TestFilesystemErrorsRecoverInSession(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(file, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(Options{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, tc := range []struct{ source, diagnostic string }{
		{`await noe.readDir("missing")`, "no such file or directory"},
		{`await noe.readDir("plain.txt")`, "not a directory"},
		{`await noe.stat("missing")`, "no such file or directory"},
	} {
		result := s.Eval(context.Background(), tc.source)
		if result.Exit || result.Err == nil {
			t.Fatalf("input %q result = %+v, want a recoverable rejected Promise", tc.source, result)
		}
		if diagnostic := Diagnostic(result.Err); !strings.Contains(diagnostic, tc.diagnostic) {
			t.Fatalf("input %q diagnostic = %q, want substring %q", tc.source, diagnostic, tc.diagnostic)
		}
		continued := s.Eval(context.Background(), `40 + 2`)
		if continued.Exit || continued.Err != nil || continued.Output != "42" {
			t.Fatalf("session did not continue after %q: %+v", tc.source, continued)
		}
	}
}

func readFilesystemEntries(t *testing.T, s *Session, stdout *bytes.Buffer, input string) []filesystemDirEntry {
	t.Helper()
	raw := evalFilesystemJSON(t, s, stdout, input)
	var records []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &records); err != nil {
		t.Fatalf("readDir JSON %q: %v", raw, err)
	}
	entries := make([]filesystemDirEntry, 0, len(records))
	for _, record := range records {
		if len(record) != 4 {
			t.Fatalf("readDir entry has unexpected properties: %s", raw)
		}
		for _, key := range []string{"name", "isFile", "isDirectory", "isSymlink"} {
			if _, ok := record[key]; !ok {
				t.Fatalf("readDir entry is missing %q: %s", key, raw)
			}
		}
		var entry filesystemDirEntry
		if err := json.Unmarshal(mustJSON(t, record), &entry); err != nil {
			t.Fatalf("readDir entry %s: %v", raw, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func readFilesystemStat(t *testing.T, s *Session, stdout *bytes.Buffer, input string) filesystemStat {
	t.Helper()
	raw := evalFilesystemJSON(t, s, stdout, input)
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("stat JSON %q: %v", raw, err)
	}
	if len(record) != 4 {
		t.Fatalf("stat result has unexpected properties: %s", raw)
	}
	for _, key := range []string{"size", "modifiedAt", "isFile", "isDirectory"} {
		if _, ok := record[key]; !ok {
			t.Fatalf("stat result is missing %q: %s", key, raw)
		}
	}
	var result filesystemStat
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("stat result %s: %v", raw, err)
	}
	return result
}

func evalFilesystemJSON(t *testing.T, s *Session, stdout *bytes.Buffer, input string) []byte {
	t.Helper()
	stdout.Reset()
	result := s.Eval(context.Background(), input)
	if result.Exit || result.Err != nil {
		t.Fatalf("input %q result = %+v", input, result)
	}
	result = s.Eval(context.Background(), `console.log(JSON.stringify(_))`)
	if result.Exit || result.Err != nil {
		t.Fatalf("JSON output after %q result = %+v", input, result)
	}
	return bytes.TrimSpace(stdout.Bytes())
}

func jsString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
