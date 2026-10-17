package agent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

type broker struct {
	host      *Host
	server    *http.Server
	url       string
	token     string
	done      chan struct{}
	closed    sync.Once
	requestMu sync.Mutex
	closing   bool
	requests  sync.WaitGroup
}

type agentRun struct {
	operation   *Operation
	ctx         context.Context
	cancel      context.CancelFunc
	started     time.Time
	config      runConfig
	parentRunID string
	childScopes map[*Operation]struct{}
}

type runConfig struct {
	Provider  string
	Model     string
	Name      string
	MaxTokens int
	Effort    string
}

type startRequest struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Name      string `json:"name"`
	MaxTokens int    `json:"maxTokens"`
	Effort    string `json:"effort,omitempty"`
}

type modelRequest struct {
	RunID     string            `json:"runId"`
	Model     string            `json:"model"`
	Messages  []json.RawMessage `json:"messages"`
	Tools     []json.RawMessage `json:"tools,omitempty"`
	CacheKey  string            `json:"cacheKey,omitempty"`
	MaxTokens int               `json:"maxTokens,omitempty"`
	Effort    string            `json:"effort,omitempty"`
}

type finishRequest struct {
	RunID  string `json:"runId"`
	Status string `json:"status"`
	Answer string `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
}

type runJSRequest struct {
	RunID      string  `json:"runId"`
	ToolCallID string  `json:"toolCallId,omitempty"`
	Code       *string `json:"code"`
}

type modelResponse struct {
	Message      json.RawMessage `json:"message"`
	FinishReason string          `json:"finishReason"`
	Usage        json.RawMessage `json:"usage,omitempty"`
}

type rpcErrorBody struct {
	Error rpcError `json:"error"`
}

type rpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RPCError is a safe broker error returned to the JS promise boundary.
// It intentionally contains no raw provider diagnostics or response body.
type RPCError struct {
	Code    string
	Message string
	cause   error
}

func (e *RPCError) Error() string {
	if e == nil || e.Message == "" {
		return "agent request failed"
	}
	return e.Message
}

func (e *RPCError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
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
		_ = b.server.Close()
		<-b.done
		b.requests.Wait()
	})
}

// Call invokes one operation-scoped broker RPC.
func (op *Operation) Call(method, payload string) (string, error) {
	if op == nil || op.host == nil {
		return "", ErrOperationClosed
	}
	if err := op.ctx.Err(); err != nil {
		return "", err
	}
	if op.initErr != nil {
		return "", op.initErr
	}
	if method != "start" && method != "model" && method != "finish" && method != "run_js" {
		return "", &RPCError{Code: "method", Message: "unknown agent request method"}
	}
	if !json.Valid([]byte(payload)) {
		return "", &RPCError{Code: "request", Message: "invalid agent request JSON"}
	}
	urlBase, token := op.remoteBase, op.remoteAuth
	if urlBase == "" {
		b, err := op.host.ensureBroker()
		if err != nil {
			return "", err
		}
		urlBase, token = b.url, b.token
	}
	if op.scope == "" || token == "" {
		return "", &RPCError{Code: "connection", Message: "agent connection is incomplete"}
	}
	if err := validateConnectionURL(urlBase); err != nil {
		return "", err
	}
	requestURL := strings.TrimRight(urlBase, "/") + "/rpc/" + method
	req, err := http.NewRequestWithContext(op.ctx, http.MethodPost, requestURL, strings.NewReader(payload))
	if err != nil {
		if ctxErr := op.ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", &RPCError{Code: "transport", Message: "agent broker request failed"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Noe-Scope", op.scope)
	response, err := op.client.Do(req)
	if err != nil {
		if ctxErr := op.ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", &RPCError{Code: "transport", Message: "agent broker request failed"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var body rpcErrorBody
		if json.NewDecoder(response.Body).Decode(&body) == nil && body.Error.Code != "" {
			err := &RPCError{Code: body.Error.Code, Message: body.Error.Message}
			if body.Error.Code == "cancelled" {
				if ctxErr := op.ctx.Err(); ctxErr != nil {
					return "", ctxErr
				}
				err.cause = context.Canceled
			}
			return "", err
		}
		return "", &RPCError{Code: "broker", Message: "agent broker rejected the request"}
	}
	var result json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || !json.Valid(result) {
		return "", &RPCError{Code: "broker", Message: "agent broker returned an invalid response"}
	}
	return string(result), nil
}

func validateConnectionURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return &RPCError{Code: "connection", Message: "agent connection URL is invalid"}
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return &RPCError{Code: "connection", Message: "agent connection URL must use a loopback address"}
		}
	}
	return nil
}

func (b *broker) serveHTTP(w http.ResponseWriter, r *http.Request) {
	b.requestMu.Lock()
	if b.closing {
		b.requestMu.Unlock()
		return
	}
	b.requests.Add(1)
	b.requestMu.Unlock()
	defer b.requests.Done()
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeRPCError(w, http.StatusMethodNotAllowed, "method", "POST required")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+b.token)) != 1 {
		writeRPCError(w, http.StatusUnauthorized, "authentication", "invalid agent capability")
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/rpc/")
	if method == r.URL.Path || (method != "start" && method != "model" && method != "finish" && method != "run_js") {
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
	if h.opts.Connection != nil || h.provider == nil {
		writeRPCError(w, http.StatusBadGateway, "provider", "agent provider is unavailable")
		return
	}
	switch method {
	case "start":
		b.handleStart(w, r, operation)
	case "model":
		b.handleModel(w, r, operation)
	case "finish":
		b.handleFinish(w, r, operation)
	case "run_js":
		b.handleRunJS(w, r, operation)
	}
}

func (b *broker) handleStart(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request startRequest
	err := decodeRequest(r, &request)
	effort, validEffort := provider.NormalizeEffort(request.Effort)
	if err != nil || request.Provider != "openrouter" || strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.Name) == "" || request.MaxTokens < 0 || !validEffort {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid start request")
		return
	}
	id, err := randomID()
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate agent run")
		return
	}
	ctx, cancel := context.WithCancel(op.ctx)
	config := runConfig{
		Provider: request.Provider, Model: request.Model,
		Name: request.Name, MaxTokens: request.MaxTokens, Effort: effort,
	}
	run := &agentRun{
		operation: op, ctx: ctx, cancel: cancel, started: time.Now(),
		config: config, parentRunID: op.parentRunID,
		childScopes: make(map[*Operation]struct{}),
	}
	h := b.host
	h.mu.Lock()
	if op.closed || op.ctx.Err() != nil || h.closed {
		h.mu.Unlock()
		cancel()
		writeRPCError(w, http.StatusConflict, "scope", "agent operation has ended")
		return
	}
	h.runs[id] = run
	h.mu.Unlock()
	h.traceEvent(traceEvent{
		RunID: id, ParentID: run.parentRunID, Name: config.Name, Model: config.Model,
		Status: "started",
	})
	h.observeRun("agent.started", id, run, events.Event{"status": "started"})
	writeJSON(w, map[string]string{"runId": id})
}

func (b *broker) handleModel(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request modelRequest
	decodeErr := decodeRequest(r, &request)
	effort, validEffort := provider.NormalizeEffort(request.Effort)
	if decodeErr != nil || request.RunID == "" || strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 || request.MaxTokens < 0 || !validEffort {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid model request")
		return
	}
	for _, message := range request.Messages {
		if !isJSONObject(message) {
			writeRPCError(w, http.StatusBadRequest, "request", "invalid model message")
			return
		}
	}
	for _, tool := range request.Tools {
		if !isJSONObject(tool) {
			writeRPCError(w, http.StatusBadRequest, "request", "invalid model tool")
			return
		}
	}
	h := b.host
	h.mu.Lock()
	run := h.runs[request.RunID]
	valid := run != nil && run.operation == op && run.ctx.Err() == nil && !op.closed && !h.closed
	h.mu.Unlock()
	if !valid {
		writeRPCError(w, http.StatusConflict, "run", "unknown agent run")
		return
	}
	if request.Model != run.config.Model || request.MaxTokens != run.config.MaxTokens || effort != run.config.Effort {
		writeRPCError(w, http.StatusBadRequest, "request", "model request does not match agent run configuration")
		return
	}
	requestID := ""
	if h.opts.Observe != nil {
		var err error
		requestID, err = randomID()
		if err != nil {
			writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate model request ID")
			return
		}
		h.observeRun("model.started", request.RunID, run, events.Event{
			"requestId": requestID, "status": "started",
		})
	}
	ctx, cancel := context.WithCancel(run.ctx)
	stopRequest := context.AfterFunc(r.Context(), cancel)
	defer func() { stopRequest(); cancel() }()
	started := time.Now()
	var observeStream func(provider.StreamEvent)
	if requestID != "" {
		observeStream = func(streamEvent provider.StreamEvent) {
			switch streamEvent.Type {
			case "thinking", "thinking_summary", "message_delta":
				h.observeRun(streamEvent.Type, request.RunID, run, events.Event{
					"requestId": requestID, "text": streamEvent.Text,
				})
			}
		}
	}
	response, err := h.provider.Generate(ctx, provider.Request{
		Model: run.config.Model, Messages: request.Messages, Tools: request.Tools,
		CacheKey: request.CacheKey, MaxTokens: run.config.MaxTokens, Effort: run.config.Effort,
		Observe: observeStream,
	})
	if err != nil {
		status, code, message := classifyProviderError(err, ctx)
		h.traceEvent(traceEvent{
			RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
			Status: "provider_error", Duration: time.Since(started),
		})
		if requestID != "" {
			modelStatus := "failed"
			if code == "cancelled" {
				modelStatus = "cancelled"
			}
			h.observeRun("model.failed", request.RunID, run, events.Event{
				"requestId": requestID, "status": modelStatus, "reason": code,
				"duration": time.Since(started),
			})
		}
		writeRPCError(w, status, code, message)
		return
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		status, code, message := classifyProviderError(ctxErr, ctx)
		h.traceEvent(traceEvent{
			RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
			Status: "cancelled", Duration: time.Since(started),
		})
		if requestID != "" {
			h.observeRun("model.failed", request.RunID, run, events.Event{
				"requestId": requestID, "status": "cancelled", "reason": "cancelled",
				"duration": time.Since(started),
			})
		}
		writeRPCError(w, status, code, message)
		return
	}
	if !json.Valid(response.Message) || len(response.Message) == 0 || (len(response.Usage) != 0 && !json.Valid(response.Usage)) {
		h.traceEvent(traceEvent{
			RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
			Status: "invalid_response", Duration: time.Since(started),
		})
		if requestID != "" {
			h.observeRun("model.failed", request.RunID, run, events.Event{
				"requestId": requestID, "status": "invalid_response", "reason": "provider_response",
				"duration": time.Since(started),
			})
		}
		writeRPCError(w, http.StatusBadGateway, "provider_response", "provider returned an invalid response")
		return
	}
	h.traceEvent(traceEvent{
		RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
		Status: "model_response", Duration: time.Since(started), Usage: summarizeUsage(response.Usage),
	})
	if requestID != "" {
		h.observeRun("model.finished", request.RunID, run, events.Event{
			"requestId": requestID, "status": "finished", "finishReason": response.FinishReason,
			"duration": time.Since(started), "usage": summarizeUsage(response.Usage),
		})
		if text, toolCallIDs, ok := intermediateMessage(response.Message); ok && strings.TrimSpace(text) != "" {
			h.observeRun("agent.message", request.RunID, run, events.Event{
				"requestId": requestID, "status": "intermediate", "text": text, "toolCallIds": toolCallIDs,
			})
		}
	}
	writeJSON(w, modelResponse{Message: response.Message, FinishReason: response.FinishReason, Usage: response.Usage})
}

func (b *broker) handleFinish(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request finishRequest
	if err := decodeRequest(r, &request); err != nil || request.RunID == "" {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid finish request")
		return
	}
	h := b.host
	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || run.operation != op {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "unknown agent run")
		return
	}
	delete(h.runs, request.RunID)
	run.cancel()
	childScopes := make([]*Operation, 0, len(run.childScopes))
	for childScope := range run.childScopes {
		childScopes = append(childScopes, childScope)
	}
	clear(run.childScopes)
	h.mu.Unlock()
	for _, childScope := range childScopes {
		childScope.Close()
	}
	status := sanitizeRunStatus(request.Status)
	if status == "completed" && strings.TrimSpace(request.Answer) != "" {
		h.observeRun("agent.final", request.RunID, run, events.Event{
			"status": "completed", "text": request.Answer,
		})
	}
	h.traceEvent(traceEvent{
		RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
		Status: status, Duration: time.Since(run.started),
	})
	fields := events.Event{
		"status": status, "duration": time.Since(run.started),
	}
	if status == "failed" && request.Error != "" {
		fields["error"] = request.Error
	}
	h.observeRun("agent."+status, request.RunID, run, fields)
	writeJSON(w, map[string]bool{})
}

func (b *broker) handleRunJS(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request runJSRequest
	if err := decodeRequest(r, &request); err != nil || request.RunID == "" || request.Code == nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid run_js request")
		return
	}
	h := b.host
	h.mu.Lock()
	run := h.runs[request.RunID]
	valid := run != nil && run.operation == op && !op.closed && !h.closed
	h.mu.Unlock()
	if !valid {
		writeRPCError(w, http.StatusConflict, "run", "unknown agent run")
		return
	}
	started := time.Now()
	startFields := events.Event{"toolCallId": request.ToolCallID, "source": "run_js"}
	if comment := firstLineComment(*request.Code); comment != "" {
		startFields["text"] = comment
	}
	h.observeRun("run_js.started", request.RunID, run, startFields)

	ctx, cancel := context.WithCancel(run.ctx)
	stopRequest := context.AfterFunc(r.Context(), cancel)
	defer func() {
		stopRequest()
		cancel()
	}()
	if ctx.Err() != nil {
		result := Result{ExitCode: -1, Reason: reasonForContext(ctx.Err())}
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
		status, code, message := classifyProviderError(ctx.Err(), ctx)
		writeRPCError(w, status, code, message)
		return
	}

	// Give this generated NOE process an independent broker scope. Register it
	// under the run before starting the process so finish can cancel descendants.
	childOperation := h.NewOperation(ctx)
	h.mu.Lock()
	registered := h.runs[request.RunID] == run && run.ctx.Err() == nil && !op.closed && !h.closed && !childOperation.closed
	if registered {
		childOperation.parentRunID = request.RunID
		run.childScopes[childOperation] = struct{}{}
	}
	h.mu.Unlock()
	if !registered {
		childOperation.Close()
		result := Result{ExitCode: -1, Reason: reasonForContext(run.ctx.Err())}
		if result.Reason == "" {
			result.Reason = "cancelled"
		}
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
		status, code, message := classifyProviderError(run.ctx.Err(), run.ctx)
		writeRPCError(w, status, code, message)
		return
	}
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			childOperation.Close()
			h.mu.Lock()
			delete(run.childScopes, childOperation)
			h.mu.Unlock()
		})
	}
	defer release()
	if opErr := childOperation.ctx.Err(); opErr != nil {
		result := Result{ExitCode: -1, Reason: reasonForContext(opErr)}
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
		status, code, message := classifyProviderError(opErr, childOperation.ctx)
		writeRPCError(w, status, code, message)
		return
	}
	if childOperation.initErr != nil {
		result := Result{Stderr: childOperation.initErr.Error(), ExitCode: -1, Reason: "failed"}
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
		writeJSON(w, result)
		return
	}
	connection := &Connection{
		URL: b.url, Token: b.token, Scope: childOperation.scope,
		Defaults: Config{
			Provider: run.config.Provider, Model: run.config.Model,
			MaxTokens: run.config.MaxTokens, Effort: run.config.Effort,
		},
	}
	result, err := (Runner{
		Executable: h.opts.Executable,
		Cwd:        h.opts.Cwd,
		Connection: connection,
	}).Run(childOperation.ctx, *request.Code)
	contextErr := ctx.Err()
	release()
	if contextErr != nil {
		result.ExitCode = -1
		result.Reason = reasonForContext(contextErr)
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
		status, code, message := classifyProviderError(contextErr, ctx)
		writeRPCError(w, status, code, message)
		return
	}
	if err != nil {
		result.Reason = "failed"
		if result.ExitCode == 0 {
			result.ExitCode = -1
		}
		if result.Stderr != "" {
			result.Stderr += "\n"
		}
		result.Stderr += err.Error()
	}
	if result.Reason == "" {
		if result.ExitCode == 0 {
			result.Reason = "completed"
		} else {
			result.Reason = "failed"
		}
	}
	b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
	writeJSON(w, result)
}

func (b *broker) finishJSResult(runID string, run *agentRun, started time.Time, toolCallID string, result Result) {
	b.traceJSResult(runID, run, started, result)
	fields := events.Event{"toolCallId": toolCallID, "source": "run_js"}
	if result.Stdout != "" {
		stdout := fieldsWith(fields, events.Event{"text": result.Stdout})
		b.host.observeRun("stdout", runID, run, stdout)
	}
	if result.Stderr != "" {
		stderr := fieldsWith(fields, events.Event{"text": result.Stderr})
		b.host.observeRun("stderr", runID, run, stderr)
	}
	b.host.observeRun("run_js.finished", runID, run, fieldsWith(fields, events.Event{
		"success":  result.Reason == "completed" && result.ExitCode == 0,
		"exitCode": result.ExitCode, "reason": result.Reason,
		"duration": time.Since(started),
	}))
}

func firstLineComment(code string) string {
	line, _, _ := strings.Cut(code, "\n")
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "//") {
		return ""
	}
	return line
}

func fieldsWith(base, additional events.Event) events.Event {
	fields := make(events.Event, len(base)+len(additional))
	for key, value := range base {
		fields[key] = value
	}
	for key, value := range additional {
		fields[key] = value
	}
	return fields
}

func intermediateMessage(raw json.RawMessage) (string, []string, bool) {
	var message struct {
		Content   json.RawMessage   `json:"content"`
		ToolCalls []json.RawMessage `json:"tool_calls"`
	}
	if json.Unmarshal(raw, &message) != nil || len(message.ToolCalls) == 0 {
		return "", nil, false
	}
	ids := make([]string, 0, len(message.ToolCalls))
	for _, rawCall := range message.ToolCalls {
		var call struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(rawCall, &call) == nil && call.ID != "" {
			ids = append(ids, call.ID)
		}
	}
	return messageText(message.Content), ids, true
}

func messageText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var builder strings.Builder
	for _, part := range parts {
		if part.Type != "text" || part.Text == "" {
			continue
		}
		if builder.Len() != 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(part.Text)
	}
	return builder.String()
}

func (b *broker) traceJSResult(runID string, run *agentRun, started time.Time, result Result) {
	exitCode := result.ExitCode
	b.host.traceEvent(traceEvent{
		RunID: runID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
		Status: "js_result", Duration: time.Since(started),
		ExitCode: &exitCode, Reason: result.Reason,
	})
}

func sanitizeRunStatus(status string) string {
	switch status {
	case "completed", "failed", "cancelled":
		return status
	default:
		return "failed"
	}
}

func decodeRequest(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return errors.New("invalid trailing JSON data")
	}
	return nil
}

func isJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

func classifyProviderError(err error, ctx context.Context) (int, string, string) {
	if ctx != nil && ctx.Err() != nil {
		return http.StatusRequestTimeout, "cancelled", "provider request cancelled"
	}
	var providerErr *provider.ProviderError
	if errors.As(err, &providerErr) {
		status := http.StatusBadGateway
		switch providerErr.Kind {
		case provider.ErrorAuthentication:
			status = http.StatusUnauthorized
		case provider.ErrorQuota:
			status = http.StatusPaymentRequired
		case provider.ErrorRateLimit:
			status = http.StatusTooManyRequests
		case provider.ErrorCancelled:
			return http.StatusRequestTimeout, "cancelled", "provider request cancelled"
		}
		return status, string(providerErr.Kind), providerErr.Error()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return http.StatusRequestTimeout, "cancelled", "provider request cancelled"
	}
	return http.StatusBadGateway, "provider", "provider request failed"
}

func writeRPCError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rpcErrorBody{Error: rpcError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, value any) {
	_ = json.NewEncoder(w).Encode(value)
}

type traceEvent struct {
	RunID    string        `json:"runId,omitempty"`
	ParentID string        `json:"parentRunId,omitempty"`
	Name     string        `json:"name,omitempty"`
	Model    string        `json:"model,omitempty"`
	Status   string        `json:"status"`
	Duration time.Duration `json:"duration,omitempty"`
	Usage    *usageSummary `json:"usage,omitempty"`
	ExitCode *int          `json:"exitCode,omitempty"`
	Reason   string        `json:"reason,omitempty"`
}

type usageSummary struct {
	PromptTokens     *int64   `json:"promptTokens,omitempty"`
	CompletionTokens *int64   `json:"completionTokens,omitempty"`
	TotalTokens      *int64   `json:"totalTokens,omitempty"`
	CachedTokens     *int64   `json:"cachedTokens,omitempty"`
	Cost             *float64 `json:"cost,omitempty"`
}

func (h *Host) traceEvent(event traceEvent) {
	if h.opts.Trace == nil {
		return
	}
	h.traceMu.Lock()
	defer h.traceMu.Unlock()
	_ = json.NewEncoder(h.opts.Trace).Encode(event)
}

func summarizeUsage(raw json.RawMessage) *usageSummary {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	var usage struct {
		PromptTokens     *int64 `json:"prompt_tokens"`
		CompletionTokens *int64 `json:"completion_tokens"`
		TotalTokens      *int64 `json:"total_tokens"`
		PromptDetails    *struct {
			CachedTokens *int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		Cost *float64 `json:"cost"`
	}
	if json.Unmarshal(raw, &usage) != nil {
		return nil
	}
	summary := &usageSummary{
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
		TotalTokens: usage.TotalTokens, Cost: usage.Cost,
	}
	if usage.PromptDetails != nil {
		summary.CachedTokens = usage.PromptDetails.CachedTokens
	}
	return summary
}
