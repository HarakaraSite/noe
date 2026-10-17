package agent

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type broker struct {
	host            *Host
	server          *http.Server
	url             string
	token           string
	done            chan struct{}
	closed          sync.Once
	requestMu       sync.Mutex
	closing         bool
	requests        sync.WaitGroup
	control         *controlRegistration
	controlRequests sync.WaitGroup
}

func (h *Host) ensureBroker() (*broker, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrHostClosed
	}
	if h.broker != nil {
		return h.broker, nil
	}
	token, err := randomID()
	if err != nil {
		return nil, fmt.Errorf("initialize agent broker capability: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start agent broker: %w", err)
	}
	b := &broker{
		host:  h,
		url:   "http://" + listener.Addr().String(),
		token: token,
		done:  make(chan struct{}),
	}
	if (h.opts.EnableControl || h.opts.ControlDir != "") && h.opts.Connection == nil {
		dir := h.opts.ControlDir
		if dir == "" {
			dir, err = DefaultControlDir()
			if err != nil {
				_ = listener.Close()
				return nil, err
			}
		}
		b.control, err = newControlRegistration(dir, b.url)
		if err != nil {
			_ = listener.Close()
			return nil, err
		}
	}
	b.server = &http.Server{
		Handler:           http.HandlerFunc(b.serveHTTP),
		ReadHeaderTimeout: 5 * time.Second,
	}
	h.broker = b
	go func() {
		defer close(b.done)
		_ = b.server.Serve(listener)
	}()
	return b, nil
}

func (b *broker) close() {
	b.closed.Do(func() {
		// Gate Add against Wait, including already accepted HTTP connections.
		b.requestMu.Lock()
		b.closing = true
		b.requestMu.Unlock()
		// Finish and flush control acknowledgements before closing connections.
		b.controlRequests.Wait()
		b.control.close()
		_ = b.server.Close()
		<-b.done
		b.requests.Wait()
	})
}

func (b *broker) serveHTTP(w http.ResponseWriter, r *http.Request) {
	b.requestMu.Lock()
	if b.closing {
		b.requestMu.Unlock()
		return
	}
	b.requests.Add(1)
	control := strings.HasPrefix(r.URL.Path, "/control/")
	if control {
		b.controlRequests.Add(1)
	}
	b.requestMu.Unlock()
	if control {
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(2 * time.Second))
		_ = controller.SetWriteDeadline(time.Now().Add(2 * time.Second))
		defer func() {
			_ = http.NewResponseController(w).Flush()
			b.controlRequests.Done()
		}()
	}
	defer b.requests.Done()
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeRPCError(w, http.StatusMethodNotAllowed, "method", "POST required")
		return
	}
	if control {
		b.serveControl(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+b.token)) != 1 {
		writeRPCError(w, http.StatusUnauthorized, "authentication", "invalid agent capability")
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/rpc/")
	if method == r.URL.Path || !validRPCMethod(method) {
		writeRPCError(w, http.StatusNotFound, "method", "unknown agent request method")
		return
	}
	var operation *Operation
	scope := r.Header.Get("X-Noe-Scope")
	h := b.host
	h.mu.Lock()
	for candidate := range h.operations {
		if candidate.scope == scope && !candidate.closed {
			operation = candidate
			break
		}
	}
	h.mu.Unlock()
	if operation == nil || operation.ctx.Err() != nil {
		writeRPCError(w, http.StatusConflict, "scope", "agent operation has ended")
		return
	}
	if !operation.beginRequest() {
		writeRPCError(w, http.StatusConflict, "scope", "agent operation has ended")
		return
	}
	defer operation.endRequest()
	if operation.ctx.Err() != nil {
		writeRPCError(w, http.StatusConflict, "scope", "agent operation has ended")
		return
	}
	if method == "decide" && h.opts.Connection == nil {
		b.handleDecide(w, r, operation)
		return
	}
	if h.opts.Connection != nil || h.provider == nil {
		writeRPCError(w, http.StatusBadGateway, "provider", "agent provider is unavailable")
		return
	}
	switch method {
	case "list_models":
		b.handleListModels(w, r)
	case "start":
		b.handleStart(w, r, operation)
	case "model":
		b.handleModel(w, r, operation)
	case "finish":
		b.handleFinish(w, r, operation)
	case "run_js":
		b.handleRunJS(w, r, operation)
	case "spawn_agent":
		b.handleSpawn(w, r, operation)
	case "observe_agent", "wait_agent":
		b.handleInstance(w, r, operation, method == "wait_agent")
	case "queue_agent":
		b.handleQueue(w, r, operation)
	case "interrupt_agent":
		b.handleInterrupt(w, r, operation)
	case "steer_agent":
		b.handleSteer(w, r, operation)
	case "apply_steers":
		b.handleApplySteers(w, r, operation)
	case "commit_final":
		b.handleCommitFinal(w, r, operation)
	}
}

func validRPCMethod(method string) bool {
	switch method {
	case "start", "model", "finish", "run_js", "spawn_agent", "observe_agent", "wait_agent", "queue_agent", "steer_agent", "apply_steers", "commit_final", "interrupt_agent", "decide", "list_models":
		return true
	}
	return false
}
