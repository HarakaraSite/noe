package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The fixture is copied to a different module so this gate checks the public
// contract from outside both the package and the checkout.
func TestExternalPortableConsumer(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate portable test source")
	}
	packageDir := filepath.Dir(sourceFile)
	root := filepath.Dir(packageDir)
	work, err := os.MkdirTemp("", "noe-agent-portable-consumer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	fixtureDir := filepath.Join(packageDir, "testdata", "consumer")
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(fixtureDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, entry.Name()), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	goMod := "module example.com/noe-agent-consumer\n\ngo 1.27.2\n\nrequire (\n\tforge.harakara.site/littleisland/noe/v2 v2.0.0-dev.0\n\tgithub.com/go-quickjs/go-quickjs v0.28.0\n)\n\nreplace forge.harakara.site/littleisland/noe/v2 => " + strconv.Quote(root) + "\n"
	goMod += "replace github.com/go-quickjs/go-quickjs => " + strconv.Quote(filepath.Join(root, "third_party", "go-quickjs")) + "\n"
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(goMod), 0600); err != nil {
		t.Fatal(err)
	}
	sums, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "go.sum"), sums, 0600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, mode := range []string{"portable", "gohost"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.CommandContext(ctx, "go", "run", "-mod=mod", ".", mode)
			cmd.Dir = work
			cmd.Env = consumerEnvironment()
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("external Go consumer failed: %v\n%s", err, output)
			}
		})
	}

}

func consumerEnvironment() []string {
	var env []string
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if !ok || strings.HasPrefix(strings.ToUpper(key), "NOE_") || strings.EqualFold(key, "GOWORK") || strings.EqualFold(key, "GOFLAGS") || strings.EqualFold(key, "CGO_ENABLED") {
			continue
		}
		env = append(env, item)
	}
	env = append(env,
		"GOWORK=off",
		"GOFLAGS=",
		"CGO_ENABLED=0",
		"GOOS="+runtime.GOOS,
		"GOARCH="+runtime.GOARCH,
	)
	return env
}
