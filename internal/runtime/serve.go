package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// installServe adds the local HTTP server API to noe. Listener and handler
// state belongs to this runtime; only request and response data crosses the
// net/http goroutine boundary.
func (h *host) installServe(noe quickjs.Value, stderr io.Writer) error {
	if stderr == nil {
		stderr = io.Discard
	}

	rt := h.rt
	optionParser, err := rt.EvalFile("<noe-serve-options>", serveOptionsFactory)
	if err != nil {
		return err
	}
	requestFactory, err := rt.EvalFile("<noe-serve-request>", serveRequestFactory)
	if err != nil {
		return err
	}
	responseParser, err := rt.EvalFile("<noe-serve-response>", serveResponseFactory)
	if err != nil {
		return err
	}
	observe, err := rt.EvalFile("<noe-serve-observer>", serveObserverFactory)
	if err != nil {
		return err
	}

	api := &serveAPI{
		h:              h,
		stderr:         stderr,
		optionParser:   optionParser,
		requestFactory: requestFactory,
		responseParser: responseParser,
		observe:        observe,
	}
	return noe.Set("serve", func(options, handler quickjs.Value) (quickjs.Value, error) {
		return api.listen(options, handler)
	})
}

type serveAPI struct {
	h              *host
	stderr         io.Writer
	optionParser   quickjs.Value
	requestFactory quickjs.Value
	responseParser quickjs.Value
	observe        quickjs.Value
}

func (a *serveAPI) listen(options, handler quickjs.Value) (quickjs.Value, error) {
	parsed, err := a.optionParser.Call(options)
	if err != nil {
		return quickjs.Value{}, err
	}
	if !handler.IsFunction() {
		return quickjs.Value{}, a.h.rt.ThrowTypeError("noe.serve handler must be a function")
	}
	hostname, err := parsed.Get("hostname")
	if err != nil {
		return quickjs.Value{}, err
	}
	port, err := parsed.Get("port")
	if err != nil {
		return quickjs.Value{}, err
	}
	address := net.JoinHostPort(hostname.String(), fmt.Sprint(port.Int()))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return quickjs.Value{}, err
	}
	managedListener := &serveListener{Listener: listener, closed: make(chan struct{})}

	state := &serveServer{api: a, handler: handler, listener: managedListener}
	state.server = &http.Server{
		BaseContext: func(net.Listener) context.Context { return state.source.ctx },
		Handler:     http.HandlerFunc(state.serveHTTP),
	}
	state.source = a.h.startEventSource(func() {
		_ = state.server.Close()
		_ = managedListener.Close()
	})

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		state.source.Abort()
		return quickjs.Value{}, fmt.Errorf("noe.serve: listener returned unsupported address %T", listener.Addr())
	}
	actualHost := addr.IP.String()
	serverValue := a.h.rt.NewObject()
	url := "http://" + net.JoinHostPort(actualHost, fmt.Sprint(addr.Port))
	if err := errors.Join(
		serverValue.Set("hostname", actualHost),
		serverValue.Set("port", addr.Port),
		serverValue.Set("url", url),
		serverValue.Set("close", state.close),
	); err != nil {
		state.source.Abort()
		return quickjs.Value{}, err
	}

	go func() {
		err := state.server.Serve(managedListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			state.source.Fail(fmt.Errorf("noe.serve listener: %w", err))
		}
	}()
	return serverValue, nil
}

type serveServer struct {
	api      *serveAPI
	handler  quickjs.Value
	listener *serveListener
	server   *http.Server
	source   *eventSource
	closeOne sync.Once
}

// close stops accepting requests synchronously, then lets net/http finish all
// accepted handlers and response writes away from the JavaScript owner.
func (s *serveServer) close() {
	s.closeOne.Do(func() {
		go func() {
			err := s.server.Shutdown(s.source.ctx)
			if err != nil {
				if s.source.ctx.Err() != nil {
					s.source.Abort()
					return
				}
				s.source.Fail(fmt.Errorf("noe.serve shutdown: %w", err))
				_ = s.server.Close()
				return
			}
			s.source.Done()
		}()
		// Shutdown closes its tracked listener before waiting for handlers.
		// Waiting for this signal guarantees that close() has stopped accepts
		// without waiting on a handler that may have called close itself.
		<-s.listener.closed
	})
}

func (s *serveServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if r.Context().Err() == nil {
			http.Error(w, "could not read request body", http.StatusBadRequest)
		}
		return
	}

	ex := &serveExchange{
		method:  r.Method,
		url:     serveRequestURL(r),
		path:    r.URL.Path,
		headers: serveRequestHeaders(r),
		body:    body,
		reply:   make(chan serveReply, 1),
		gone:    make(chan struct{}),
	}
	defer close(ex.gone)

	s.source.Post(func() { s.handle(ex) })
	select {
	case result := <-ex.reply:
		if result.err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		for _, header := range result.headers {
			w.Header().Add(header.name, header.value)
		}
		w.WriteHeader(result.status)
		if r.Method != http.MethodHead && result.status != http.StatusNoContent && result.status != http.StatusNotModified {
			_, _ = w.Write(result.body)
		}
	case <-r.Context().Done():
	case <-s.source.ctx.Done():
	}
}

type serveListener struct {
	net.Listener
	once   sync.Once
	closed chan struct{}
	err    error
}

func (l *serveListener) Close() error {
	l.once.Do(func() {
		l.err = l.Listener.Close()
		close(l.closed)
	})
	return l.err
}
