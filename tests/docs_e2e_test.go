//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"fmt"
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
	writeFixture(t, filepath.Join(dir, "config.json"), "{}")
	t.Setenv("NOE_CONFIG_FILE", filepath.Join(dir, "config.json"))
	for _, tc := range []struct{ topic, member string }{{"noe:sockets", "connect"}, {"noe:http", "fetch"}, {"noe:http", "serve"}} {
		t.Run(tc.topic+"/"+tc.member, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "docs", tc.topic, tc.member)
			cmd.Dir = dir
			cmd.Env = envWith(os.Environ(), "NOE_CONFIG_FILE", filepath.Join(dir, "invalid.json"))
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil || stderr.Len() != 0 {
				t.Fatalf("standalone docs: %v stderr=%s", err, &stderr)
			}
			if !strings.Contains(stdout.String(), "## "+tc.member) {
				t.Fatal("missing API reference detail")
			}
			code := fmt.Sprintf("// Read the bundled API contract.\nconst {docs}=await import(\"noe:docs\");console.log(docs(%q,%q));", tc.topic, tc.member)
			result := runAgentJS(t, agent.Runner{Executable: binary, Cwd: dir}, code)
			if result.ExitCode != 0 || result.Reason != "completed" || result.Stderr != "" || strings.TrimSpace(result.Stdout) != strings.TrimSpace(stdout.String()) {
				t.Fatalf("CLI/run_js reference differs: %+v", result)
			}
		})
	}
}
