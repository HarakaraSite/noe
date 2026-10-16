package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type scope struct {
	ctx       context.Context
	parentRun string
}
type agentRun struct {
	ctx                 context.Context
	cancel              context.CancelFunc
	scope, parent, name string
}
type host struct {
	cfg                           config
	ctx                           context.Context
	cancel                        context.CancelFunc
	p                             provider
	dir, url, token, serviceToken string
	server                        *http.Server
	done                          chan struct{}
	mu                            sync.Mutex
	next                          int
	scopes                        map[string]scope
	runs                          map[string]*agentRun
	traceMu                       sync.Mutex
	trace                         *json.Encoder
}
type jsResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
	Reason   string `json:"reason"`
}
type rpcRequest struct {
	RunID    string            `json:"runId"`
	Name     string            `json:"name"`
	Status   string            `json:"status"`
	Code     string            `json:"code"`
	Query    string            `json:"query"`
	CacheKey string            `json:"cacheKey"`
	Messages []json.RawMessage `json:"messages"`
	Tools    []json.RawMessage `json:"tools"`
}

func startHost(parent context.Context, cfg config, p provider, trace io.Writer) (*host, error) {
	ctx, cancel := context.WithCancel(parent)
	h := &host{cfg: cfg, ctx: ctx, cancel: cancel, p: p, scopes: map[string]scope{"root": {ctx: ctx}}, runs: map[string]*agentRun{}, trace: json.NewEncoder(trace), done: make(chan struct{})}
	var err error
	h.dir, err = os.MkdirTemp("", "noe-agent-loop-")
	if err != nil {
		cancel()
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			cancel()
			os.RemoveAll(h.dir)
		}
	}()
	err = fs.WalkDir(assets, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		dest := filepath.Join(h.dir, path)
		if d.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0600)
	})
	if err != nil {
		return nil, err
	}
	if err = os.Mkdir(filepath.Join(h.dir, "scripts"), 0700); err != nil {
		return nil, err
	}
	token := make([]byte, 32)
	if _, err = rand.Read(token); err != nil {
		return nil, err
	}
	h.token = hex.EncodeToString(token)
	if _, err = rand.Read(token); err != nil {
		return nil, err
	}
	h.serviceToken = hex.EncodeToString(token)
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	h.url = "http://" + l.Addr().String()
	h.server = &http.Server{Handler: http.HandlerFunc(h.handle), ReadHeaderTimeout: 5 * time.Second}
	go func() { defer close(h.done); _ = h.server.Serve(l) }()
	h.event("host_start", map[string]any{"provider": cfg.provider, "model": cfg.model})
	failed = false
	return h, nil
}

func (h *host) close() {
	h.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = h.server.Shutdown(ctx)
	_ = h.server.Close()
	<-h.done
	h.event("host_end", nil)
	_ = os.RemoveAll(h.dir)
}

func (h *host) event(kind string, fields map[string]any) {
	h.traceMu.Lock()
	defer h.traceMu.Unlock()
	if fields == nil {
		fields = map[string]any{}
	}
	fields["kind"], fields["time"] = kind, time.Now().UTC().Format(time.RFC3339Nano)
	_ = h.trace.Encode(fields)
}

func (h *host) id(prefix string) string { h.next++; return fmt.Sprintf("%s-%d", prefix, h.next) }

// A JS process may exit without reaching an agent's finally block. Discard its
// run records and cancel work owned by those runs when its scope ends.
func (h *host) endScope(id string) {
	h.mu.Lock()
	delete(h.scopes, id)
	var abandoned []string
	for runID, run := range h.runs {
		if run.scope == id {
			run.cancel()
			delete(h.runs, runID)
			abandoned = append(abandoned, runID)
		}
	}
	h.mu.Unlock()
	for _, runID := range abandoned {
		h.event("run_end", map[string]any{"runId": runID, "status": "aborted"})
	}
}

func (h *host) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeError(w, 405, "method", "POST required")
		return
	}
	if r.URL.Path == "/fixture/search" {
		h.serveSearchFixture(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.token)) != 1 {
		writeError(w, 401, "bridge_auth", "invalid host capability")
		return
	}
	var q rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeError(w, 400, "request", err.Error())
		return
	}
	scopeID := r.Header.Get("X-Noe-Scope")
	h.mu.Lock()
	s, ok := h.scopes[scopeID]
	h.mu.Unlock()
	if !ok || s.ctx.Err() != nil {
		writeError(w, 409, "scope", "execution has ended")
		return
	}
	if r.URL.Path == "/run/start" {
		ctx, cancel := context.WithCancel(s.ctx)
		h.mu.Lock()
		id := h.id("run")
		h.runs[id] = &agentRun{ctx: ctx, cancel: cancel, scope: scopeID, parent: s.parentRun, name: q.Name}
		h.mu.Unlock()
		h.event("run_start", map[string]any{"runId": id, "parentRunId": s.parentRun, "scope": scopeID, "name": q.Name})
		writeJSON(w, map[string]any{"runId": id})
		return
	}
	h.mu.Lock()
	run, ok := h.runs[q.RunID]
	h.mu.Unlock()
	if !ok || (run.scope != scopeID && !(r.URL.Path == "/service/search" && q.RunID == s.parentRun)) {
		writeError(w, 409, "run", "unknown run in this execution")
		return
	}
	if r.URL.Path == "/run/finish" {
		run.cancel()
		h.mu.Lock()
		delete(h.runs, q.RunID)
		h.mu.Unlock()
		h.event("run_end", map[string]any{"runId": q.RunID, "parentRunId": run.parent, "status": q.Status})
		writeJSON(w, map[string]any{})
		return
	}
	ctx, cancel := context.WithCancel(run.ctx)
	stop := context.AfterFunc(r.Context(), cancel)
	defer func() { stop(); cancel() }()
	if err := ctx.Err(); err != nil {
		writeError(w, 409, "cancelled", err.Error())
		return
	}
	switch r.URL.Path {
	case "/model":
		start := time.Now()
		h.event("model_start", map[string]any{"runId": q.RunID, "cacheKey": q.CacheKey, "messages": len(q.Messages)})
		result, err := h.p.generate(ctx, modelRequest{Messages: q.Messages, Tools: q.Tools, CacheKey: q.CacheKey, Name: run.name})
		fields := map[string]any{"runId": q.RunID, "elapsedMs": time.Since(start).Milliseconds()}
		if err != nil {
			fields["status"] = "error"
			h.event("model_end", fields)
			var pe *providerError
			if errors.As(err, &pe) {
				writeError(w, 502, pe.Code, pe.Message)
			} else {
				writeError(w, 502, "provider", err.Error())
			}
			return
		}
		fields["status"], fields["finishReason"], fields["usage"] = "ok", result.FinishReason, result.Usage
		h.event("model_end", fields)
		writeJSON(w, result)
	case "/js":
		h.mu.Lock()
		id := h.id("js")
		h.scopes[id] = scope{ctx: ctx, parentRun: q.RunID}
		h.mu.Unlock()
		defer h.endScope(id)
		path := filepath.Join(h.dir, "scripts", id+".js")
		prelude := "import {createAgent} from '../lib/agent.js';\nimport {webSearch} from '../lib/services.js';\n"
		if err := os.WriteFile(path, []byte(prelude+q.Code), 0600); err != nil {
			writeError(w, 500, "execution", err.Error())
			return
		}
		hash := sha256.Sum256([]byte(q.Code))
		h.event("js_start", map[string]any{"runId": q.RunID, "scope": id, "codeSHA256": hex.EncodeToString(hash[:])})
		result := h.execute(ctx, id, path, nil)
		h.event("js_end", map[string]any{"runId": q.RunID, "scope": id, "exitCode": result.ExitCode, "reason": result.Reason, "stdoutBytes": len(result.Stdout), "stderrBytes": len(result.Stderr)})
		writeJSON(w, result)
	case "/service/search":
		// A deterministic authenticated fixture, not an Internet search engine.
		if err := ctx.Err(); err != nil {
			writeError(w, 409, "cancelled", err.Error())
			return
		}
		h.event("service_search", map[string]any{"runId": q.RunID, "query": q.Query})
		result, err := h.searchFixture(ctx, q.Query)
		if err != nil {
			writeError(w, 502, "service", err.Error())
			return
		}
		writeJSON(w, result)
	default:
		writeError(w, 404, "method", "unknown host method")
	}
}

func writeJSON(w http.ResponseWriter, value any) { _ = json.NewEncoder(w).Encode(value) }
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	writeJSON(w, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (h *host) execute(ctx context.Context, scopeID, path string, args []string) jsResult {
	cmd := exec.CommandContext(ctx, h.cfg.noe, append([]string{"run", path}, args...)...)
	cmd.Dir = h.cfg.workdir
	// The key is read from disk by this host; provider-key environment variables
	// are also excluded from child environments. This is not an OS sandbox.
	for _, v := range os.Environ() {
		name, _, _ := strings.Cut(v, "=")
		switch name {
		case "OPENROUTER_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY":
			continue
		}
		if strings.HasPrefix(name, "NOE_AGENT_") {
			continue
		}
		cmd.Env = append(cmd.Env, v)
	}
	h.mu.Lock()
	owner := h.scopes[scopeID].parentRun
	h.mu.Unlock()
	cmd.Env = append(cmd.Env, "NOE_AGENT_HOST="+h.url, "NOE_AGENT_TOKEN="+h.token, "NOE_AGENT_SCOPE="+scopeID, "NOE_AGENT_OWNER_RUN="+owner,
		"NOE_AGENT_MODEL="+h.cfg.model, "NOE_AGENT_PROVIDER="+h.cfg.provider, "NOE_AGENT_MAX_STEPS="+strconv.Itoa(h.cfg.maxSteps), "NOE_AGENT_FIXTURES="+filepath.Join(h.dir, "fixtures"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	defer h.endScope(scopeID)
	var out, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &stderr
	result := jsResult{Reason: "completed"}
	err := cmd.Start()
	if err == nil {
		pid := cmd.Process.Pid
		defer syscall.Kill(-pid, syscall.SIGKILL)
		err = cmd.Wait()
	}
	result.Stdout, result.Stderr = out.String(), stderr.String()
	if err != nil {
		result.ExitCode, result.Reason = 1, "failed"
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() >= 0 {
			result.ExitCode = exit.ExitCode()
		}
		if result.Stderr == "" {
			result.Stderr = err.Error() + "\n"
		}
	}
	if ctx.Err() != nil {
		result.ExitCode, result.Reason = 130, "cancelled"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.ExitCode, result.Reason = 124, "timeout"
		}
	}
	return result
}
