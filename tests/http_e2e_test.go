//go:build e2e

package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNoeHTTPExamplesThroughCLI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/data.json":
			io.WriteString(w, `{"value":42}`)
		case "/bytes":
			w.Write([]byte{0, 255, 120})
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			io.Copy(w, r.Body)
		}
	}))
	defer server.Close()
	work := t.TempDir()
	jsonPath, binaryPath := filepath.Join(work, "data.json"), filepath.Join(work, "data.bin")
	for _, args := range [][]string{
		{"roundtrip.js"},
		{"fetch-json.js", server.URL + "/data.json", jsonPath},
		{"download-file.js", server.URL + "/bytes", binaryPath},
		{"http-json.js", "PATCH", server.URL + "/echo", `{"value":21}`},
	} {
		t.Run(args[0], func(t *testing.T) {
			cmd := append([]string{"run", filepath.Join(repoRootPath, "examples", "http", args[0])}, args[1:]...)
			got := runCLI(t, work, nil, cmd...)
			requireExit(t, got, 0)
			if got.stderr != "" {
				t.Fatal(got.stderr)
			}
			if args[0] == "roundtrip.js" && got.stdout != "{\"value\":42}\n" {
				t.Fatalf("roundtrip: %+v", got)
			}
			if args[0] == "http-json.js" && got.stdout != "{\"status\":201,\"ok\":true,\"body\":{\"value\":21}}\n" {
				t.Fatalf("JSON request: %+v", got)
			}
		})
	}
	data, err := os.ReadFile(jsonPath)
	var value struct{ Value int }
	if err != nil || json.Unmarshal(data, &value) != nil || value.Value != 42 {
		t.Fatalf("saved JSON: %q err=%v", data, err)
	}
	data, err = os.ReadFile(binaryPath)
	if err != nil || string(data) != string([]byte{0, 255, 120}) {
		t.Fatalf("saved bytes: %v err=%v", data, err)
	}
}
