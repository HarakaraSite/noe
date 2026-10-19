//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"noe/internal/agent"
)

func TestDocsStandaloneBinaryAndRunJS(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, filepath.Base(cliPath))
	src, err := os.Open(cliPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(binary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy binary: %v, close: %v", copyErr, closeErr)
	}
	writeFixture(t, filepath.Join(dir, "invalid.json"), "invalid configuration")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "docs", "noe:sockets", "connect")
	cmd.Dir = dir
	cmd.Env = envWith(os.Environ(), "NOE_CONFIG_FILE", filepath.Join(dir, "invalid.json"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stderr.Len() != 0 {
		t.Fatalf("standalone docs: %v stderr=%s", err, &stderr)
	}
	if !strings.Contains(stdout.String(), "connect") {
		t.Fatal("missing API reference")
	}
	writeFixture(t, filepath.Join(dir, "config.json"), "{}")
	t.Setenv("NOE_CONFIG_FILE", filepath.Join(dir, "config.json"))
	result := runAgentJS(t, agent.Runner{Executable: binary, Cwd: dir}, "// Read the bundled socket connection contract.\nconst {docs}=await import(\"noe:docs\");console.log(docs(\"noe:sockets\",\"connect\"));")
	if result.ExitCode != 0 || result.Reason != "completed" || result.Stderr != "" || strings.TrimSpace(result.Stdout) != strings.TrimSpace(stdout.String()) {
		t.Fatalf("CLI/run_js reference differs: %+v", result)
	}
}
