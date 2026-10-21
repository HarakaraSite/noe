package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestControlRegistrationUsesPrivateFileAndRemovesIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	registration, err := newControlRegistration(dir, "http://127.0.0.1:12345")
	if err != nil {
		t.Fatal(err)
	}
	defer registration.close()
	info, err := os.Stat(registration.path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("capability file mode=%v", info.Mode())
	}
	data, err := os.ReadFile(registration.path)
	if err != nil {
		t.Fatal(err)
	}
	var saved ControlConnection
	if json.Unmarshal(data, &saved) != nil || saved != registration.connection || saved.Version != 1 || saved.HostID == "" || saved.Token == "" || saved.HostID == saved.Token {
		t.Fatalf("invalid control registration")
	}
	if strings.Contains(registration.path, saved.Token) {
		t.Fatal("control token appears in public discovery filename")
	}
	registration.close()
	if _, err := os.Stat(registration.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("closed host still registered: %v", err)
	}
}

func TestDiscoverControlHostsAndResolveUnambiguousIDs(t *testing.T) {
	const secret = "human-control-capability"
	listing := ControlListing{HostID: "host-a", PID: 123, Agents: []AgentSnapshot{{AgentID: "abcd-one", CurrentRunID: "run-one", Name: "first", State: "running"}, {AgentID: "abcd-two", Name: "second", State: "idle"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/control/agents" || r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("wrong control endpoint or capability")
			http.Error(w, "unauthorized", 401)
			return
		}
		_ = json.NewEncoder(w).Encode(listing)
	}))
	defer server.Close()
	dir := t.TempDir()
	connection := ControlConnection{Version: 1, HostID: "host-a", PID: 123, URL: server.URL, Token: secret}
	encoded, _ := json.Marshal(connection)
	if err := os.WriteFile(filepath.Join(dir, "host-a.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "partial.json"), []byte(`{"token":`), 0600); err != nil {
		t.Fatal(err)
	}
	stale := httptest.NewServer(http.NotFoundHandler())
	stale.Close()
	old := connection
	old.HostID = "stale"
	old.URL = stale.URL
	encoded, _ = json.Marshal(old)
	if err := os.WriteFile(filepath.Join(dir, "stale.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	hosts, err := DiscoverControls(context.Background(), dir)
	if err != nil || len(hosts) != 1 || len(hosts[0].Listing.Agents) != 2 {
		t.Fatalf("discovery: hosts=%d err=%v", len(hosts), err)
	}
	got, snapshot, err := ResolveControlTarget(context.Background(), dir, "run-on", true)
	if err != nil || got.HostID != "host-a" || snapshot.CurrentRunID != "run-one" {
		t.Fatalf("resolve run: host=%q id=%q err=%v", got.HostID, snapshot.CurrentRunID, err)
	}
	if _, _, err := ResolveControlTarget(context.Background(), dir, "abcd", false); err == nil || !strings.Contains(err.Error(), "ambiguous agent ID") || !strings.Contains(err.Error(), "abcd-one (first)") || !strings.Contains(err.Error(), "abcd-two (second)") {
		t.Fatalf("ambiguous ID accepted: %v", err)
	}
	if _, _, err := ResolveControlTarget(context.Background(), dir, "absent", false); err == nil || !strings.Contains(err.Error(), "no active") {
		t.Fatalf("missing ID: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.json")); err != nil {
		t.Fatal("discovery deleted another process's record")
	}
}

func TestControlClientDoesNotForwardCapabilityThroughRedirect(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	_, err := (ControlConnection{HostID: "redirect", URL: redirect.URL, Token: "secret"}).Call(context.Background(), "agents", map[string]any{})
	if err == nil || reached {
		t.Fatalf("control capability followed redirect: err=%v reached=%v", err, reached)
	}
}

func TestControlClientCancellationStopsStatusProbe(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (ControlConnection{HostID: "blocked", URL: server.URL, Token: "secret"}).Call(ctx, "agents", map[string]any{})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("control probe ignored cancellation")
	}
}

func TestDefaultControlDirectoryPrecedence(t *testing.T) {
	explicit, xdg := t.TempDir(), t.TempDir()
	t.Setenv(ControlDirEnv, explicit)
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	got, err := DefaultControlDir()
	if err != nil || got != explicit {
		t.Fatalf("explicit directory=%q %v", got, err)
	}
	t.Setenv(ControlDirEnv, "")
	got, err = DefaultControlDir()
	if err != nil || got != filepath.Join(xdg, "noe") {
		t.Fatalf("XDG runtime directory=%q %v", got, err)
	}
}
