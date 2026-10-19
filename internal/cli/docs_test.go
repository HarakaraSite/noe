package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noe/internal/apidocs"
)

func TestDocsBypassesAgentConfiguration(t *testing.T) {
	dir := t.TempDir()
	badConfig := filepath.Join(dir, "config.json")
	if err := os.WriteFile(badConfig, []byte("invalid json"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOE_CONFIG_FILE", badConfig)
	t.Setenv("NOE_OPENROUTER_API_KEY_FILE", filepath.Join(dir, "absent-key"))
	want, err := apidocs.Lookup("node:child_process", "execFile")
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"docs", "node:child_process", "execFile"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 || stdout.String() != want {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
	}
}

func TestDocsUsageAndErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"docs"}, 0},
		{[]string{"docs", "--help"}, 0},
		{[]string{"docs", "node:not-a-module"}, 2},
		{[]string{"docs", "node:fs", "not-a-detail"}, 2},
		{[]string{"docs", "one", "two", "three"}, 2},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), tc.args, nil, &stdout, &stderr)
		if code != tc.code || (code == 0 && (stdout.Len() == 0 || stderr.Len() != 0)) || (code != 0 && (stdout.Len() != 0 || stderr.Len() == 0)) {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", tc.args, code, &stdout, &stderr)
		}
		if code == 0 && !strings.Contains(stdout.String(), "noe docs") {
			t.Fatal("missing navigation")
		}
	}
}
