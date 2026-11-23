package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	jsonv1 "encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

// openAIWebSocket is owned by one RunScope and one OpenAI adapter. It keeps
// only the server continuation ID and fingerprints of accepted input items.
type openAIWebSocket struct {
	owner *OpenAI

	gate     chan struct{}
	closedCh chan struct{}

	mu         sync.Mutex
	closed     bool
	connection *responses.ResponseConnection
	responseID string
	history    [][32]byte
	config     [32]byte
	dialCancel context.CancelFunc
	dialID     uint64
	nextCallID uint64
	activeCall uint64
}

func newOpenAIWebSocket(owner *OpenAI) *openAIWebSocket {
	return &openAIWebSocket{owner: owner, gate: make(chan struct{}, 1), closedCh: make(chan struct{})}
}

func (p *OpenAI) generateManagedWebSocket(ctx context.Context, request Request, scope *RunScope) (Response, error) {
	resource, err := scope.resource(p, func() managedResource { return newOpenAIWebSocket(p) })
	if err != nil {
		return Response{}, err
	}
	transport, ok := resource.(*openAIWebSocket)
	if !ok {
		return Response{}, providerError(ErrorResponse, 0, nil)
	}
	return generateWithRetry(ctx, request, func(ctx context.Context, request Request) (Response, error) {
		return p.generateManagedWebSocketOnce(ctx, request, transport)
	})
}

func (p *OpenAI) generateManagedWebSocketOnce(ctx context.Context, request Request, transport *openAIWebSocket) (Response, error) {
	if err := cancellationError(ctx); err != nil {
		return Response{}, err
	}
	payload, err := configuredResponsesRequestBody(p.profile, request)
	if err != nil {
		return Response{}, providerError(ErrorResponse, 0, nil)
	}
	fields, input, err := splitOpenAIWebSocketRequest(payload)
	if err != nil {
		return Response{}, providerError(ErrorResponse, 0, nil)
	}
	inputHashes, err := fingerprintItems(input)
	if err != nil {
		return Response{}, providerError(ErrorResponse, 0, nil)
	}
	configHash, err := responsesConfigurationHash(fields)
	if err != nil {
		return Response{}, providerError(ErrorResponse, 0, nil)
	}

	// The handshake keeps only its configured session identity. The SDK
	// connection outlives this Generate call, so it must not retain payloads or
	// user observation callbacks through the request context.
	state := &callState{sessionID: request.sessionID()}
	if err := transport.acquire(ctx); err != nil {
		return Response{}, err
	}
	callID, err := transport.beginCall(ctx)
	if err != nil {
		transport.release()
		return Response{}, err
	}
	defer transport.releaseCall(callID)
	defer transport.release()
	stopAbort := context.AfterFunc(ctx, func() { transport.abortCall(callID) })
	defer stopAbort()

	connection, err := transport.ensureConnection(ctx, state)
	if err != nil {
		transport.drop()
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return Response{}, cancelErr
		}
		if closedErr := transport.closedError(); closedErr != nil {
			return Response{}, closedErr
		}
		if isPreviousResponseCacheMiss(err) {
			return Response{}, providerError(ErrorResponse, http.StatusBadRequest, nil)
		}
		return Response{}, err
	}
	command, incremental, err := transport.command(fields, input, inputHashes, configHash)
	if err != nil {
		transport.drop()
		return Response{}, providerError(ErrorResponse, 0, nil)
	}
	response, responseID, outputHashes, err := sendOpenAIResponse(ctx, connection, command, request.Observe)
	if err != nil && incremental && isPreviousResponseCacheMiss(err) {
		transport.drop()
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return Response{}, cancelErr
		}
		connection, err = transport.ensureConnection(ctx, state)
		if err == nil {
			command, err = encodeOpenAIWebSocketCommand(fields, input, "")
		}
		if err == nil {
			response, responseID, outputHashes, err = sendOpenAIResponse(ctx, connection, command, request.Observe)
		}
	}
	if err != nil {
		transport.drop()
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return Response{}, cancelErr
		}
		if closedErr := transport.closedError(); closedErr != nil {
			return Response{}, closedErr
		}
		if isPreviousResponseCacheMiss(err) {
			return Response{}, providerError(ErrorResponse, http.StatusBadRequest, nil)
		}
		return Response{}, err
	}
	if err := cancellationError(ctx); err != nil {
		transport.drop()
		return Response{}, err
	}
	if responseID == "" {
		// A refusal is terminal to the run and cannot extend cached history.
		transport.drop()
		return response, nil
	}
	acceptedHistory := make([][32]byte, 0, len(inputHashes)+len(outputHashes))
	acceptedHistory = append(acceptedHistory, inputHashes...)
	acceptedHistory = append(acceptedHistory, outputHashes...)
	if err := transport.accept(ctx, connection, responseID, configHash, acceptedHistory); err != nil {
		transport.drop()
		return Response{}, err
	}
	return response, nil
}

func (w *openAIWebSocket) acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return cancellationError(ctx)
	case <-w.closedCh:
		return providerError(ErrorCancelled, 0, context.Canceled)
	case w.gate <- struct{}{}:
	}
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		w.release()
		return providerError(ErrorCancelled, 0, context.Canceled)
	}
	return nil
}

func (w *openAIWebSocket) release() { <-w.gate }

func (w *openAIWebSocket) beginCall(ctx context.Context) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, providerError(ErrorCancelled, 0, context.Canceled)
	}
	if err := cancellationError(ctx); err != nil {
		return 0, err
	}
	w.nextCallID++
	w.activeCall = w.nextCallID
	return w.activeCall, nil
}

func (w *openAIWebSocket) releaseCall(callID uint64) {
	w.mu.Lock()
	if w.activeCall == callID {
		w.activeCall = 0
	}
	w.mu.Unlock()
}

func (w *openAIWebSocket) ensureConnection(ctx context.Context, state *callState) (*responses.ResponseConnection, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, providerError(ErrorCancelled, 0, context.Canceled)
	}
	if err := cancellationError(ctx); err != nil {
		w.mu.Unlock()
		return nil, err
	}
	if w.connection != nil {
		connection := w.connection
		w.mu.Unlock()
		return connection, nil
	}
	dialCtx, cancel := context.WithCancel(ctx)
	w.dialID++
	dialID := w.dialID
	w.dialCancel = cancel
	w.mu.Unlock()

	if client, ok := borrowedWebSocketHTTPClient(w.owner.socketHTTP.client); !ok || client == nil {
		cancel()
		w.clearDial(dialID)
		return nil, providerError(ErrorResponse, 0, nil)
	}
	token, err := w.owner.openAIToken(dialCtx)
	if err != nil {
		cancel()
		w.clearDial(dialID)
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return nil, cancelErr
		}
		if closedErr := w.closedError(); closedErr != nil {
			return nil, closedErr
		}
		return nil, providerError(ErrorAuthentication, 0, nil)
	}
	connectCtx := context.WithValue(dialCtx, callContextKey{}, state)
	var handshakeResponse *http.Response
	configureRequest := option.WithMiddleware(func(request *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		configured, err := configureOpenAIWebSocketRequest(w.owner.socketHTTP, request)
		if err != nil {
			return nil, err
		}
		return next(configured)
	})
	connection, err := w.owner.socketClient.Responses.Connect(connectCtx, responses.ResponseConnectionOptions{},
		option.WithAPIKey(token), option.WithResponseInto(&handshakeResponse), configureRequest,
	)
	cancel()
	w.clearDial(dialID)
	if err != nil {
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return nil, cancelErr
		}
		if closedErr := w.closedError(); closedErr != nil {
			return nil, closedErr
		}
		if handshakeResponse != nil && handshakeResponse.StatusCode != http.StatusSwitchingProtocols {
			return nil, state.classifyHTTPError(err)
		}
		return nil, classifyOpenAIWebSocketError(err)
	}

	w.mu.Lock()
	if w.closed || ctx.Err() != nil || w.connection != nil {
		closed := w.closed
		w.mu.Unlock()
		connection.Abort()
		if closed {
			return nil, providerError(ErrorCancelled, 0, context.Canceled)
		}
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return nil, cancelErr
		}
		return nil, providerError(ErrorTransport, 0, nil)
	}
	w.connection = connection
	w.mu.Unlock()
	return connection, nil
}

func (w *openAIWebSocket) clearDial(dialID uint64) {
	w.mu.Lock()
	if w.dialID == dialID {
		w.dialCancel = nil
	}
	w.mu.Unlock()
}

func (w *openAIWebSocket) command(fields map[string]jsonv1.RawMessage, input []jsonv1.RawMessage, inputHashes [][32]byte, config [32]byte) ([]byte, bool, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, false, providerError(ErrorCancelled, 0, context.Canceled)
	}
	previousID := w.responseID
	previousConfig := w.config
	previousHistory := w.history
	w.mu.Unlock()

	// Fingerprint comparison and JSON marshaling run outside the lifecycle lock
	// so scope Close can always detach/abort a socket promptly.
	incremental := previousID != "" && previousConfig == config && extendsFingerprints(previousHistory, inputHashes)
	if incremental {
		input = input[len(previousHistory):]
	} else {
		previousID = ""
	}
	encoded, err := encodeOpenAIWebSocketCommand(fields, input, previousID)
	return encoded, incremental, err
}

func encodeOpenAIWebSocketCommand(fields map[string]jsonv1.RawMessage, input []jsonv1.RawMessage, previousID string) ([]byte, error) {
	wire := make(map[string]any, len(fields)+3)
	for key, value := range fields {
		wire[key] = value
	}
	wire["type"] = "response.create"
	wire["input"] = input
	if previousID != "" {
		wire["previous_response_id"] = previousID
	}
	return jsonv1.Marshal(wire)
}

func (w *openAIWebSocket) accept(ctx context.Context, connection *responses.ResponseConnection, responseID string, config [32]byte, history [][32]byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return providerError(ErrorCancelled, 0, context.Canceled)
	}
	if err := cancellationError(ctx); err != nil {
		return err
	}
	if w.connection != connection || responseID == "" {
		return providerError(ErrorStream, 0, nil)
	}
	w.responseID = responseID
	w.config = config
	w.history = history
	return nil
}

func (w *openAIWebSocket) closedError() error {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return providerError(ErrorCancelled, 0, context.Canceled)
	}
	return nil
}

func (w *openAIWebSocket) abortCall(callID uint64) {
	w.mu.Lock()
	if w.closed || w.activeCall != callID {
		w.mu.Unlock()
		return
	}
	connection := w.connection
	w.connection = nil
	w.clearHistoryLocked()
	cancel := w.dialCancel
	w.dialCancel = nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if connection != nil {
		connection.Abort()
	}
}

func (w *openAIWebSocket) drop() {
	w.mu.Lock()
	connection := w.connection
	w.connection = nil
	w.clearHistoryLocked()
	cancel := w.dialCancel
	w.dialCancel = nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if connection != nil {
		connection.Abort()
	}
}

func (w *openAIWebSocket) clearHistoryLocked() {
	w.responseID = ""
	w.history = nil
	w.config = [32]byte{}
}

func (w *openAIWebSocket) Close() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	close(w.closedCh)
	connection := w.connection
	w.connection = nil
	w.clearHistoryLocked()
	cancel := w.dialCancel
	w.dialCancel = nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if connection != nil {
		connection.Abort()
	}
}

func (p *OpenAI) openAIToken(ctx context.Context) (string, error) {
	var token string
	var err error
	switch p.profile.Auth {
	case "chatgpt-oauth":
		token, err = p.oauth.Token(ctx)
	case "none":
	default:
		token, err = p.key.apiKey()
	}
	return token, err
}

func sendOpenAIResponse(ctx context.Context, connection *responses.ResponseConnection, command []byte, observe func(StreamEvent)) (Response, string, [][32]byte, error) {
	if err := connection.SendRaw(ctx, command); err != nil {
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return Response{}, "", nil, cancelErr
		}
		return Response{}, "", nil, providerError(ErrorTransport, 0, nil)
	}
	accumulator := &responsesAccumulator{}
	for {
		raw, err := connection.RecvRaw(ctx)
		if err != nil {
			if cancelErr := cancellationError(ctx); cancelErr != nil {
				return Response{}, "", nil, cancelErr
			}
			return Response{}, "", nil, providerError(ErrorStream, 0, nil)
		}
		var event struct {
			Type     string            `json:"type"`
			Response jsonv1.RawMessage `json:"response"`
		}
		if err := jsonv1.Unmarshal(raw, &event); err != nil || event.Type == "" {
			return Response{}, "", nil, providerError(ErrorResponse, 0, nil)
		}
		if err := accumulator.consumeEvent(raw); err != nil {
			if accumulator.failureKind != "" {
				return Response{}, "", nil, providerError(accumulator.failureKind, 0, nil)
			}
			return Response{}, "", nil, err
		}
		if observe != nil {
			for _, observation := range responsesObservations(raw) {
				observe(observation)
			}
		}
		switch event.Type {
		case "error":
			return Response{}, "", nil, openAIWebSocketAPIError(raw)
		case "response.failed", "response.incomplete", "response.completed":
			if accumulator.failureKind != "" {
				return Response{}, "", nil, providerError(accumulator.failureKind, 0, nil)
			}
			response, err := accumulator.response()
			if err != nil {
				return Response{}, "", nil, err
			}
			outputHashes, refused, err := responseNativeOutputFingerprints(response)
			if err != nil {
				return Response{}, "", nil, providerError(ErrorResponse, 0, nil)
			}
			if refused {
				return response, "", nil, nil
			}
			var terminal struct {
				ID string `json:"id"`
			}
			if jsonv1.Unmarshal(event.Response, &terminal) != nil || terminal.ID == "" {
				return Response{}, "", nil, providerError(ErrorResponse, 0, nil)
			}
			return response, terminal.ID, outputHashes, nil
		}
	}
}

type openAIWebSocketErrorPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

type openAIWebSocketErrorEvent struct {
	Status int                         `json:"status"`
	Code   string                      `json:"code"`
	Error  openAIWebSocketErrorPayload `json:"error"`
}

type previousResponseCacheMiss struct{}

func (previousResponseCacheMiss) Error() string { return "previous response cache was not found" }

func openAIWebSocketAPIError(raw []byte) error {
	if previousResponseError(raw) {
		return previousResponseCacheMiss{}
	}
	var event openAIWebSocketErrorEvent
	if jsonv1.Unmarshal(raw, &event) != nil {
		return providerError(ErrorResponse, 0, nil)
	}
	if event.Status == http.StatusUnauthorized || event.Status == http.StatusForbidden {
		return providerError(ErrorAuthentication, event.Status, nil)
	}
	if event.Error.Code == "websocket_connection_limit_reached" {
		// The stream retry opens a fresh connection and resends the current history.
		return providerError(ErrorStream, event.Status, nil)
	}
	if kind := providerFailureCodeKind(event.Code, event.Error.Code, event.Error.Type); kind != "" {
		return providerError(kind, event.Status, nil)
	}
	if event.Status != 0 {
		return classifyHTTPResponseError(event.Status, errors.New("provider websocket error event"), "", nil)
	}
	return providerError(ErrorResponse, 0, nil)
}

func isPreviousResponseCacheMiss(err error) bool {
	_, ok := err.(previousResponseCacheMiss)
	return ok
}

func classifyOpenAIWebSocketError(err error) error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		var headers http.Header
		if apiErr.Response != nil {
			headers = apiErr.Response.Header
		}
		return classifyHTTPResponseError(apiErr.StatusCode, err, apiErr.Code, headers)
	}
	return providerError(ErrorTransport, 0, nil)
}

func openAIErrorCode(err error) string {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr != nil {
		if apiErr.Code != "" {
			return apiErr.Code
		}
		return apiErr.Type
	}
	return ""
}

func openAIWebSocketError(raw []byte) (int, openAIWebSocketErrorPayload) {
	var event openAIWebSocketErrorEvent
	_ = jsonv1.Unmarshal(raw, &event)
	return event.Status, event.Error
}

func decodeRawObject(raw []byte) (map[string]jsonv1.RawMessage, error) {
	var object map[string]jsonv1.RawMessage
	if err := jsonv1.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, errors.New("expected a JSON object")
	}
	return object, nil
}

func splitOpenAIWebSocketRequest(payload []byte) (map[string]jsonv1.RawMessage, []jsonv1.RawMessage, error) {
	fields, err := decodeRawObject(payload)
	if err != nil {
		return nil, nil, err
	}
	var input []jsonv1.RawMessage
	if err := jsonv1.Unmarshal(fields["input"], &input); err != nil || input == nil {
		return nil, nil, errors.New("expected an input array")
	}
	// Only these remaining fields participate in configuration matching and
	// are shared by the incremental command and a full replay.
	for _, key := range [...]string{"input", "previous_response_id", "stream", "background", "type"} {
		delete(fields, key)
	}
	return fields, input, nil
}

func fingerprintItems(items []jsonv1.RawMessage) ([][32]byte, error) {
	hashes := make([][32]byte, 0, len(items))
	for _, item := range items {
		fingerprint, err := canonicalJSONFingerprint(item)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, fingerprint)
	}
	return hashes, nil
}

func canonicalJSONFingerprint(raw []byte) ([32]byte, error) {
	decoder := jsonv1.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return [32]byte{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return [32]byte{}, errors.New("trailing JSON value")
	}
	canonical, err := jsonv1.Marshal(value)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256Bytes(canonical), nil
}

func responsesConfigurationHash(fields map[string]jsonv1.RawMessage) ([32]byte, error) {
	raw, err := jsonv1.Marshal(fields)
	if err != nil {
		return [32]byte{}, err
	}
	return canonicalJSONFingerprint(raw)
}

func sha256Bytes(data []byte) [32]byte { return sha256.Sum256(data) }

func extendsFingerprints(previous, current [][32]byte) bool {
	if len(current) <= len(previous) {
		return false
	}
	for index := range previous {
		if previous[index] != current[index] {
			return false
		}
	}
	return true
}

func responseNativeOutputFingerprints(response Response) ([][32]byte, bool, error) {
	message, err := decodeRawObject(response.Message)
	if err != nil {
		return nil, false, err
	}
	if raw, exists := message["refusal"]; exists {
		var refusal string
		if jsonv1.Unmarshal(raw, &refusal) == nil && strings.TrimSpace(refusal) != "" {
			return nil, true, nil
		}
	}
	var items []jsonv1.RawMessage
	if err := jsonv1.Unmarshal(message[nativeResponseItems], &items); err != nil {
		return nil, false, err
	}
	hashes, err := fingerprintItems(items)
	return hashes, false, err
}

func previousResponseError(raw []byte) bool {
	status, failure := openAIWebSocketError(raw)
	return failure.Code == "previous_response_not_found" ||
		(status == http.StatusBadRequest && failure.Param == "previous_response_id") ||
		(status == http.StatusBadRequest && failure.Type == "invalid_request_error" && failure.Message == "Invalid previous_response_id")
}

var _ managedResource = (*openAIWebSocket)(nil)
