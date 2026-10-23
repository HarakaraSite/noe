package stdlib

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const publicModule = "forge.harakara.site/littleisland/noe/v2"

func TestExternalConsumerPortable(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate consumer test source")
	}
	root, err := filepath.Abs(filepath.Dir(filepath.Dir(source)))
	if err != nil {
		t.Fatal(err)
	}
	consumerDir, err := os.MkdirTemp("", "noe-stdlib-consumer-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(consumerDir) })
	if withinDirectory(consumerDir, root) {
		t.Fatalf("external consumer directory %q is inside checkout %q", consumerDir, root)
	}

	sources, err := filepath.Glob(filepath.Join(root, "stdlib", "testdata", "consumer", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("consumer fixture has no Go source")
	}
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(consumerDir, filepath.Base(source)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	goMod := fmt.Sprintf("module example.com/noe-stdlib-consumer\n\ngo 1.27.2\n\nrequire (\n\t%s v2.0.0-dev.0\n\tgithub.com/go-quickjs/go-quickjs v0.27.0\n)\n\nreplace %s => %q\n", publicModule, publicModule, filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(consumerDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	goSum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(consumerDir, "go.sum"), goSum, 0o644); err != nil {
		t.Fatal(err)
	}

	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	env := consumerEnvironment(map[string]string{
		"GOWORK":      "off",
		"GOFLAGS":     "",
		"GOOS":        runtime.GOOS,
		"GOARCH":      runtime.GOARCH,
		"CGO_ENABLED": "0",
	})
	for _, args := range [][]string{{"mod", "tidy"}, {"run", ".", "portable"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		cmd := exec.CommandContext(ctx, goBinary, args...)
		cmd.Dir = consumerDir
		cmd.Env = env
		output, runErr := cmd.CombinedOutput()
		cancel()
		if runErr != nil {
			t.Fatalf("external consumer command `go %s` failed (GOOS=%s GOARCH=%s CGO_ENABLED=0): %v\n%s", strings.Join(args, " "), runtime.GOOS, runtime.GOARCH, runErr, output)
		}
	}
}

func consumerEnvironment(overrides map[string]string) []string {
	filtered := make([]string, 0, len(os.Environ())+len(overrides))
	for _, value := range os.Environ() {
		name, _, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		if _, replace := overrides[name]; !replace {
			filtered = append(filtered, value)
		}
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		filtered = append(filtered, key+"="+overrides[key])
	}
	return filtered
}

func withinDirectory(path, directory string) bool {
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
