package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestREPLHistoryPersistsLogicalInputsAndPreservesOtherSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noe", "history.jsonl")
	t.Setenv("NOE_REPL_HISTORY", path)
	var stderr bytes.Buffer
	first, second := newREPLHistory(&stderr), newREPLHistory(&stderr)
	first.append("let amount = 42")
	second.append("function twice(n) {\n  return n * 2;\n}")
	first.append("twice(amount)")
	want := []string{"let amount = 42", "function twice(n) {\n  return n * 2;\n}", "twice(amount)"}
	if got := newREPLHistory(&stderr).load(); !reflect.DeepEqual(got, want) || stderr.Len() != 0 {
		t.Fatalf("loaded=%q stderr=%q", got, &stderr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "\n") != 3 {
		t.Fatalf("physical lines: %q", data)
	}
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatalf("stat=%v err=%v", stat, err)
	}
}

func TestREPLHistoryDisabledCorruptAndSaveFailure(t *testing.T) {
	var stderr bytes.Buffer
	t.Setenv("NOE_REPL_HISTORY", "")
	h := newREPLHistory(&stderr)
	h.append("42")
	if h.path != "" || len(h.load()) != 0 || stderr.Len() != 0 {
		t.Fatal("disabled history used storage")
	}
	path := filepath.Join(t.TempDir(), "history.jsonl")
	valid, _ := json.Marshal("console.log('日本語')")
	original := append([]byte("bad entry\n"), append(valid, '\n')...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOE_REPL_HISTORY", path)
	h = newREPLHistory(&stderr)
	if got := h.load(); !reflect.DeepEqual(got, []string{"console.log('日本語')"}) || !strings.Contains(stderr.String(), "invalid history") {
		t.Fatalf("loaded=%q stderr=%q", got, &stderr)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, original) {
		t.Fatal("corrupt input file was altered")
	}
	stderr.Reset()
	t.Setenv("NOE_REPL_HISTORY", t.TempDir()) // a directory cannot be an appendable history file
	h = newREPLHistory(&stderr)
	h.append("42")
	if !h.failed || stderr.Len() == 0 {
		t.Fatalf("failed=%v stderr=%q", h.failed, &stderr)
	}
	n := stderr.Len()
	h.append("43")
	if stderr.Len() != n {
		t.Fatal("save failure was repeatedly reported")
	}
}

func TestREPLHistoryAppendsAfterUnterminatedFinalRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(path, []byte(`"40 + 2"`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOE_REPL_HISTORY", path)
	var stderr bytes.Buffer
	h := newREPLHistory(&stderr)
	h.append("42 + 1")
	if got := h.load(); !reflect.DeepEqual(got, []string{"40 + 2", "42 + 1"}) || stderr.Len() != 0 {
		t.Fatalf("history=%q stderr=%q", got, &stderr)
	}
}
