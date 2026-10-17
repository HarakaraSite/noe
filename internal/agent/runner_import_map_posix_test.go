//go:build !windows

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"noe/internal/modulemap"
)

func TestRunnerPrivateImportMapLifetime(t *testing.T) {
	for _, mode := range []string{"success", "failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			snapshot, err := modulemap.Parse([]byte(`{"imports":{"app/lib":"./lib.mjs"}}`), dir)
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(dir, "ready")
			body := `test "$1" = run && test "$2" = --import-map || exit 90
test -f "$3" && test -f "$4" || exit 91
printf '%s\n' "$3"
cat "$3"
printf '\n'
printf ready > ready
`
			switch mode {
			case "failure":
				body += "exit 7\n"
			case "cancel":
				body += "exec sleep 60\n"
			}
			executable := writeFakeNOE(t, body)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stopped := make(chan struct{})
			if mode == "cancel" {
				go func() {
					defer close(stopped)
					ticker := time.NewTicker(time.Millisecond)
					defer ticker.Stop()
					for {
						if _, err := os.Stat(marker); err == nil {
							cancel()
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
						}
					}
				}()
			} else {
				close(stopped)
			}
			result, err := (Runner{Executable: executable, Cwd: dir, ImportMap: snapshot}).Run(ctx, `console.log("unused by fixture")`)
			<-stopped
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.SplitN(result.Stdout, "\n", 2)
			if len(lines) != 2 || !filepath.IsAbs(lines[0]) {
				t.Fatalf("result=%+v", result)
			}
			restored, err := modulemap.Parse([]byte(strings.TrimSpace(lines[1])), dir)
			if err != nil {
				t.Fatal(err)
			}
			target, err := restored.Resolve("app/lib", "")
			if err != nil || target != filepath.Join(dir, "lib.mjs") {
				t.Fatalf("target=%q err=%v", target, err)
			}
			if _, err := os.Stat(filepath.Dir(lines[0])); !os.IsNotExist(err) {
				t.Fatalf("private snapshot directory remains: %v", err)
			}
			if mode == "success" && result.Reason != "completed" || mode == "failure" && result.ExitCode != 7 || mode == "cancel" && result.Reason != "cancelled" {
				t.Fatalf("mode=%s result=%+v", mode, result)
			}
		})
	}
}
