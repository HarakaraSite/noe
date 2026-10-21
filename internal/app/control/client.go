package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent"
)

const (
	ControlDirEnv  = "NOE_AGENT_RUNTIME_DIR"
	controlTimeout = 2 * time.Second
	maxControlBody = 1 << 20
)

type AgentSnapshot = agent.InstanceSnapshot

// ControlConnection is stored in a private discovery file. Its bearer token
// grants human control and is separate from any agent or runtime capability.
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

type RPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	if e == nil || e.Message == "" {
		return "agent control request failed"
	}
	return e.Message
}

type rpcErrorBody struct {
	Error RPCError `json:"error"`
}

type controlRegistration struct {
	connection ControlConnection
	path       string
}

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

func randomID() (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func newControlRegistration(dir, endpoint string) (*controlRegistration, error) {
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
	connection := ControlConnection{Version: 1, HostID: hostID, PID: os.Getpid(), URL: endpoint, Token: token}
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

// Call sends one bounded admission or status request. It never waits for an
// accepted task to finish.
func (c ControlConnection) Call(ctx context.Context, method string, payload any) (json.RawMessage, error) {
	if ctx == nil {
		return nil, errors.New("agent control request requires a context")
	}
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
	requestCtx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/control/"+method, bytes.NewReader(encoded))
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
		if json.NewDecoder(io.LimitReader(response.Body, maxControlBody)).Decode(&body) == nil && body.Error.Code != "" {
			return nil, &body.Error
		}
		return nil, fmt.Errorf("agent host rejected control request (HTTP %d)", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxControlBody+1))
	if err != nil || len(data) > maxControlBody || !json.Valid(data) {
		return nil, errors.New("agent host returned an invalid control response")
	}
	return json.RawMessage(data), nil
}

func validateConnectionURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Opaque != "" || parsed.Fragment != "" {
		return errors.New("agent control URL is invalid")
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("agent control URL must use a loopback address")
		}
	}
	if parsed.Port() == "" {
		return errors.New("agent control URL is invalid")
	}
	return nil
}

// DiscoverControls probes saved hosts concurrently and ignores stale records.
// Records belonging to other processes are never removed by discovery.
func DiscoverControls(ctx context.Context, dir string) ([]DiscoveredHost, error) {
	if ctx == nil {
		return nil, errors.New("agent discovery requires a context")
	}
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
		if err != nil || len(data) > maxControlBody {
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

// ResolveControlTarget accepts a full ID or an unambiguous prefix and returns
// the snapshot used for the selected target.
func ResolveControlTarget(ctx context.Context, dir, id string, run bool) (ControlConnection, AgentSnapshot, error) {
	if strings.TrimSpace(id) == "" {
		return ControlConnection{}, AgentSnapshot{}, errors.New("agent target ID is required")
	}
	hosts, err := DiscoverControls(ctx, dir)
	if err != nil {
		return ControlConnection{}, AgentSnapshot{}, err
	}
	type match struct {
		connection ControlConnection
		snapshot   AgentSnapshot
		id         string
	}
	var matches []match
	for _, host := range hosts {
		for _, snapshot := range host.Listing.Agents {
			candidate := snapshot.AgentID
			if run {
				candidate = snapshot.CurrentRunID
			}
			if candidate != "" && strings.HasPrefix(candidate, id) {
				matches = append(matches, match{connection: host.Connection, snapshot: snapshot, id: candidate})
			}
		}
	}
	if len(matches) == 0 {
		return ControlConnection{}, AgentSnapshot{}, fmt.Errorf("no active %s matches ID %q", targetName(run), id)
	}
	if len(matches) > 1 {
		var candidates []string
		for _, matched := range matches {
			candidates = append(candidates, fmt.Sprintf("  %s (%s)", matched.id, matched.snapshot.Name))
		}
		return ControlConnection{}, AgentSnapshot{}, fmt.Errorf("ambiguous %s ID %q; use a longer ID:\n%s", targetName(run), id, strings.Join(candidates, "\n"))
	}
	return matches[0].connection, matches[0].snapshot, nil
}

func targetName(run bool) string {
	if run {
		return "run"
	}
	return "agent"
}
