package control

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent"
)

const maxRequestBytes = 1 << 20

type Options struct {
	Host      *agent.Host
	Directory func() (string, error)
	Observe   func(agent.Event)
	Cancel    context.CancelFunc
}

type Controller struct {
	ctx       context.Context
	cancel    context.CancelFunc
	opts      Options
	wake      chan struct{}
	done      chan struct{}
	activated chan struct{}
	closeOnce sync.Once

	mu       sync.Mutex
	accepted bool
	closing  bool
	err      error
}

// New allocates an idle controller. It does not resolve a directory, open a
// listener, or write discovery state until Accept is called.
func New(ctx context.Context, opts Options) *Controller {
	if ctx == nil {
		ctx = context.Background()
	}
	lifeCtx, cancel := context.WithCancel(ctx)
	c := &Controller{ctx: lifeCtx, cancel: cancel, opts: opts, wake: make(chan struct{}, 1), done: make(chan struct{}), activated: make(chan struct{})}
	go c.run()
	return c
}

// Accept requests first-time activation without blocking the agent admission
// path. Repeated calls are harmless.
func (c *Controller) Accept() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.closing || c.accepted || c.ctx.Err() != nil {
		c.mu.Unlock()
		return
	}
	c.accepted = true
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Close stops activation and HTTP admission, waits for activation and active
// requests to finish, and removes this controller's discovery record.
func (c *Controller) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closing = true
		accepted := c.accepted
		c.mu.Unlock()
		select {
		case c.wake <- struct{}{}:
		default:
		}
		if !accepted {
			c.cancel()
		}
	})
	c.mu.Lock()
	accepted := c.accepted
	c.mu.Unlock()
	if accepted {
		select {
		case <-c.activated:
			c.cancel()
		case <-c.done:
		}
	}
	<-c.done
	c.mu.Lock()
	err := c.err
	c.mu.Unlock()
	return err
}

func (c *Controller) run() {
	defer close(c.done)
	for {
		c.mu.Lock()
		accepted, closing := c.accepted, c.closing
		c.mu.Unlock()
		if accepted {
			c.activateAndServe()
			return
		}
		if closing || c.ctx.Err() != nil {
			return
		}
		select {
		case <-c.ctx.Done():
			return
		case <-c.wake:
		}
	}
}

func (c *Controller) activateAndServe() {
	activationResolved := false
	resolveActivation := func() {
		if !activationResolved {
			activationResolved = true
			close(c.activated)
		}
	}
	if c.opts.Host == nil {
		c.fail(errors.New("agent control requires a live Host"))
		resolveActivation()
		return
	}
	directory := c.opts.Directory
	if directory == nil {
		directory = DefaultControlDir
	}
	dir, err := directory()
	if err != nil {
		c.fail(err)
		resolveActivation()
		return
	}
	if err := c.ctx.Err(); err != nil {
		resolveActivation()
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		c.fail(fmt.Errorf("start agent control listener: %w", err))
		resolveActivation()
		return
	}
	endpoint := "http://" + listener.Addr().String()
	registration, err := newControlRegistration(dir, endpoint)
	if err != nil {
		_ = listener.Close()
		c.fail(err)
		resolveActivation()
		return
	}
	if c.ctx.Err() != nil {
		registration.close()
		_ = listener.Close()
		resolveActivation()
		return
	}
	server := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { c.serveHTTP(registration, w, r) }),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErr := make(chan error, 1)
	serveDone := make(chan struct{})
	go func() {
		serveErr <- server.Serve(listener)
		close(serveDone)
	}()
	resolveActivation()
	if c.opts.Observe != nil {
		c.opts.Observe(agent.Event{"type": "control.ready", "hostId": registration.connection.HostID, "pid": registration.connection.PID, "url": endpoint})
	}
	select {
	case <-c.ctx.Done():
		c.shutdown(server, serveDone, registration)
	case <-serveDone:
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.fail(fmt.Errorf("agent control listener stopped: %w", err))
		}
		c.shutdown(server, serveDone, registration)
	}
}

func (c *Controller) shutdown(server *http.Server, serveDone <-chan struct{}, registration *controlRegistration) {
	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout+time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
		if c.ctx.Err() == nil {
			c.fail(fmt.Errorf("stop agent control listener: %w", err))
		}
	}
	select {
	case <-serveDone:
	case <-ctx.Done():
		_ = server.Close()
		<-serveDone
	}
	registration.close()
}

func (c *Controller) fail(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
	if c.opts.Cancel != nil {
		c.opts.Cancel()
	}
}

func (c *Controller) serveHTTP(registration *controlRegistration, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRPCError(w, http.StatusMethodNotAllowed, "method", "POST required")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+registration.connection.Token)) != 1 {
		writeRPCError(w, http.StatusUnauthorized, "authentication", "invalid control capability")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), controlTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(controlTimeout))
	_ = controller.SetWriteDeadline(time.Now().Add(controlTimeout))
	w.Header().Set("Content-Type", "application/json")
	switch strings.TrimPrefix(r.URL.Path, "/control/") {
	case "agents":
		var request struct{}
		if !decodeRequest(w, r, &request) {
			return
		}
		agents, err := c.opts.Host.ListAgents(r.Context())
		if err != nil {
			writeHostError(w, err)
			return
		}
		active := make([]AgentSnapshot, 0, len(agents))
		for _, snapshot := range agents {
			if snapshot.State != "stopped" && snapshot.State != "failed" {
				active = append(active, snapshot)
			}
		}
		sort.Slice(active, func(i, j int) bool { return active[i].AgentID < active[j].AgentID })
		writeJSON(w, ControlListing{HostID: registration.connection.HostID, PID: registration.connection.PID, Agents: active})
	case "queue":
		var request agent.QueueRequest
		if !decodeRequest(w, r, &request) {
			return
		}
		response, err := c.opts.Host.Queue(r.Context(), request)
		if err != nil {
			writeHostError(w, err)
			return
		}
		writeJSON(w, response)
	case "steer":
		var request agent.SteerRequest
		if !decodeRequest(w, r, &request) {
			return
		}
		response, err := c.opts.Host.Steer(r.Context(), request)
		if err != nil {
			writeHostError(w, err)
			return
		}
		writeJSON(w, response)
	case "interrupt":
		c.handleInterrupt(w, r)
	default:
		writeRPCError(w, http.StatusNotFound, "method", "unknown control request method")
	}
}

func (c *Controller) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RunID   string `json:"runId,omitempty"`
		AgentID string `json:"agentId,omitempty"`
	}
	if !decodeRequest(w, r, &request) {
		return
	}
	hasRun := strings.TrimSpace(request.RunID) != ""
	hasAgent := strings.TrimSpace(request.AgentID) != ""
	if hasRun == hasAgent {
		writeRPCError(w, http.StatusBadRequest, "request", "specify exactly one of runId or agentId")
		return
	}
	if hasAgent {
		response, err := c.opts.Host.StopAgent(r.Context(), agent.StopAgentRequest{AgentID: request.AgentID})
		if err != nil {
			writeHostError(w, err)
			return
		}
		writeJSON(w, map[string]string{"target": "agent", "status": response.Status, "agentId": response.AgentID})
		return
	}
	agents, err := c.opts.Host.ListAgents(r.Context())
	if err != nil {
		writeHostError(w, err)
		return
	}
	var target *AgentSnapshot
	for index := range agents {
		if agents[index].CurrentRunID == request.RunID {
			if target != nil {
				writeRPCError(w, http.StatusConflict, "run", "run ID matched more than one agent")
				return
			}
			target = &agents[index]
		}
	}
	if target == nil || target.CurrentCommandID == "" {
		writeRPCError(w, http.StatusConflict, "run", "unknown or finished agent run")
		return
	}
	response, err := c.opts.Host.CancelTask(r.Context(), agent.CancelTaskRequest{AgentID: target.AgentID, CommandID: target.CurrentCommandID})
	if err != nil {
		writeHostError(w, err)
		return
	}
	writeJSON(w, map[string]string{"target": "run", "status": response.Status, "runId": request.RunID})
}

func decodeRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid control request")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid control request")
		return false
	}
	return true
}

func writeHostError(w http.ResponseWriter, err error) {
	status, code := http.StatusConflict, "agent"
	if errors.Is(err, context.Canceled) {
		status, code = http.StatusRequestTimeout, "cancelled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		status, code = http.StatusGatewayTimeout, "timeout"
	}
	writeRPCError(w, status, code, err.Error())
}

func writeRPCError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rpcErrorBody{Error: RPCError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, value any) {
	_ = json.NewEncoder(w).Encode(value)
}
