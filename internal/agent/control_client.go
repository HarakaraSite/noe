package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const ControlDirEnv = "NOE_AGENT_RUNTIME_DIR"

// AgentSnapshot is shared by live JS observation and the human control client.
type AgentSnapshot = instanceSnapshot

// ControlConnection is stored only in a user-private discovery file. Its token
// is distinct from the scoped capability inherited by generated JS processes.
type ControlConnection struct {
	Version int    `json:"version"`
	HostID  string `json:"hostId"`
	PID     int    `json:"pid"`
	URL     string `json:"url"`
	Token   string `json:"token"`
}

type ControlListing struct {
	HostID string          `json:"hostId"`
	PID    int             `json:"pid"`
	Agents []AgentSnapshot `json:"agents"`
}

type DiscoveredHost struct {
	Connection ControlConnection
	Listing    ControlListing
}

type controlRegistration struct {
	connection ControlConnection
	path       string
}

// DefaultControlDir does not create a directory or read any credentials.
func DefaultControlDir() (string, error) {
	if dir := os.Getenv(ControlDirEnv); dir != "" {
		return filepath.Abs(dir)
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "noe"), nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("agent runtime directory: %w", err)
	}
	return filepath.Join(dir, "noe", "run"), nil
}

func newControlRegistration(dir, url string) (*controlRegistration, error) {
	hostID, err := randomID()
	if err != nil {
		return nil, err
	}
	token, err := randomID()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create agent runtime directory: %w", err)
	}
	connection := ControlConnection{Version: 1, HostID: hostID, PID: os.Getpid(), URL: url, Token: token}
	encoded, err := json.Marshal(connection)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, hostID+".json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("register agent host: %w", err)
	}
	_, writeErr := file.Write(append(encoded, '\n'))
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("register agent host: %w", err)
	}
	return &controlRegistration{connection: connection, path: path}, nil
}
func (r *controlRegistration) close() {
	if r != nil {
		_ = os.Remove(r.path)
	}
}

// Call sends an admission/status request, never waits for a task to finish.
// A bounded probe prevents stale files whose port was reused by an unrelated
// listener from holding discovery or an interactive control command forever.
func (c ControlConnection) Call(ctx context.Context, method string, payload any) (json.RawMessage, error) {
	if err := validateConnectionURL(c.URL); err != nil {
		return nil, err
	}
	if c.Token == "" {
		return nil, errors.New("missing agent control capability")
	}
	switch method {
	case "agents", "steer", "queue", "interrupt":
	default:
		return nil, errors.New("unknown agent control method")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/control/"+method, strings.NewReader(string(encoded)))
	if err != nil {
		return nil, errors.New("invalid agent control request")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if method != "agents" {
			return nil, fmt.Errorf("agent host %s did not acknowledge the request; it may have been accepted", c.HostID)
		}
		return nil, fmt.Errorf("agent host %s is unavailable", c.HostID)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var body rpcErrorBody
		if json.NewDecoder(response.Body).Decode(&body) == nil && body.Error.Code != "" {
			return nil, &RPCError{Code: body.Error.Code, Message: body.Error.Message}
		}
		return nil, fmt.Errorf("agent host rejected control request (HTTP %d)", response.StatusCode)
	}
	var result json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || !json.Valid(result) {
		return nil, errors.New("agent host returned an invalid control response")
	}
	return result, nil
}

// DiscoverControls probes saved hosts concurrently and ignores stale records.
// It does not delete records belonging to another process.
func DiscoverControls(ctx context.Context, dir string) ([]DiscoveredHost, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []DiscoveredHost{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read agent runtime directory: %w", err)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	hosts := make([]DiscoveredHost, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var connection ControlConnection
		if json.Unmarshal(data, &connection) != nil || connection.Version != 1 || connection.HostID == "" || connection.Token == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := connection.Call(ctx, "agents", map[string]any{})
			if err != nil {
				return
			}
			var listing ControlListing
			if json.Unmarshal(result, &listing) != nil || listing.HostID != connection.HostID {
				return
			}
			mu.Lock()
			hosts = append(hosts, DiscoveredHost{Connection: connection, Listing: listing})
			mu.Unlock()
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Connection.HostID < hosts[j].Connection.HostID })
	return hosts, nil
}

// ResolveControlTarget accepts full or unambiguous abbreviated IDs.
func ResolveControlTarget(ctx context.Context, dir, id string, run bool) (ControlConnection, string, error) {
	if strings.TrimSpace(id) == "" {
		return ControlConnection{}, "", errors.New("agent target ID is required")
	}
	hosts, err := DiscoverControls(ctx, dir)
	if err != nil {
		return ControlConnection{}, "", err
	}
	type match struct {
		connection ControlConnection
		id         string
		name       string
	}
	var matches []match
	for _, host := range hosts {
		for _, agent := range host.Listing.Agents {
			candidate := agent.AgentID
			if run {
				candidate = agent.CurrentRunID
			}
			if candidate != "" && strings.HasPrefix(candidate, id) {
				matches = append(matches, match{host.Connection, candidate, agent.Name})
			}
		}
	}
	if len(matches) == 0 {
		return ControlConnection{}, "", fmt.Errorf("no active agent matches ID %q", id)
	}
	if len(matches) > 1 {
		kind := "agent"
		if run {
			kind = "run"
		}
		var candidates []string
		for _, match := range matches {
			candidates = append(candidates, fmt.Sprintf("  %s (%s)", match.id, match.name))
		}
		return ControlConnection{}, "", fmt.Errorf("ambiguous %s ID %q; use a longer ID:\n%s", kind, id, strings.Join(candidates, "\n"))
	}
	return matches[0].connection, matches[0].id, nil
}
