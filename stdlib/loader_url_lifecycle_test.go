package stdlib_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestModuleHTTPCancelHeaderAndBodyWaits(t *testing.T) {
	for _, stage := range []string{"headers", "body"} {
		for _, source := range []string{"context", "Runtime.Cancel"} {
			t.Run(stage+"/"+source, func(t *testing.T) {
				entered, ended := make(chan struct{}), make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if stage == "body" {
						w.Header().Set("Content-Type", "text/plain")
						fmt.Fprint(w, "export ")
						w.(http.Flusher).Flush()
					}
					close(entered)
					<-r.Context().Done()
					close(ended)
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				runtime, err := stdlib.NewRuntime(ctx, stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{HTTP: &stdlib.ModuleHTTPOptions{Client: server.Client()}})})
				if err != nil {
					t.Fatal(err)
				}
				defer runtime.Close()
				cancelled := make(chan bool, 1)
				go func() {
					select {
					case <-entered:
						cancelled <- true
					case <-ctx.Done():
						cancelled <- false
					}
					if source == "context" {
						cancel()
					} else {
						runtime.Cancel()
					}
				}()
				_, err = runtime.Run(stdlib.Source{Name: "[HTTP cancellation]", Virtual: true, Module: true, Code: fmt.Sprintf("import %q;", server.URL)})
				if !<-cancelled || !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("synchronous %s cancellation error=%v", stage, err)
				}
				select {
				case <-ended:
				case <-time.After(time.Second):
					t.Fatal("HTTP request survived execution cancellation")
				}
			})
		}
	}
}

func TestModuleLoaderWorkerEntryPureResolutionAndDependencyHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "export const value=42;")
	}))
	defer server.Close()
	provider := stdlib.ModuleLoader(stdlib.LoaderOptions{Imports: map[string]string{"remote-entry": server.URL}, HTTP: &stdlib.ModuleHTTPOptions{Client: server.Client()}})
	for _, entry := range []string{server.URL, "remote-entry"} {
		loader, err := provider.NewLoader(t.Context(), stdlib.Origin{Worker: true, Entry: entry, Cwd: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := loader(entry, "/worker-entry.mjs"); err == nil || !strings.Contains(err.Error(), "Worker entry URLs are unsupported") {
			t.Fatalf("Worker URL entry %q error=%v", entry, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("Worker entry rejection connected to HTTP")
	}

	root := t.TempDir()
	worker := fmt.Sprintf(`import {value} from %q;postMessage(value);close();`, server.URL)
	if err := os.WriteFile(filepath.Join(root, "worker.mjs"), []byte(worker), 0600); err != nil {
		t.Fatal(err)
	}
	profile := stdlib.Profile{
		Environment: stdlib.Environment{Cwd: "/"},
		Modules:     stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: true}, Workers: &stdlib.WorkerOptions{Max: 1}},
		Loader:      stdlib.ModuleLoader(stdlib.LoaderOptions{Imports: map[string]string{"local-entry": "./worker.mjs"}, HTTP: &stdlib.ModuleHTTPOptions{Client: server.Client()}}),
	}
	result := executeURLTest(t, profile, `const w=new Worker("local-entry",{type:"module"});export default await new Promise((ok,fail)=>{w.onmessage=e=>ok(e.data);w.onerror=e=>fail(Error(e.message));});`)
	if string(result.Data) != "42" || requests.Load() != 1 {
		t.Fatalf("local Worker alias/dependency data=%s requests=%d", result.Data, requests.Load())
	}
}

func TestPublicURLImportCapabilityMatchesProviderConfiguration(t *testing.T) {
	for _, tc := range []struct {
		profile stdlib.Profile
		want    stdlib.URLImportSupport
	}{
		{stdlib.Profile{}, stdlib.URLImportsDisabled},
		{stdlib.Profile{NewLoader: stdlib.FileLoader(nil)}, stdlib.URLImportsUnknown},
		{stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{})}, stdlib.URLImportsDisabled},
		{stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{HTTP: &stdlib.ModuleHTTPOptions{}})}, stdlib.URLImportsEnabled},
	} {
		if got := stdlib.URLImportCapability(tc.profile); got != tc.want {
			t.Errorf("capability=%q want %q", got, tc.want)
		}
	}
}
