package runtime

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	quickjs "github.com/go-quickjs/go-quickjs"
)

const serveOptionsFactory = `options => {
  if (options === null || typeof options !== "object" || Array.isArray(options)) {
    throw new TypeError("noe.serve options must be an object");
  }
  for (const name of Object.keys(options)) {
    if (name !== "hostname" && name !== "port") {
      throw new TypeError("unsupported noe.serve option: " + name);
    }
  }
  const hostname = options.hostname === undefined ? "127.0.0.1" : options.hostname;
  if (typeof hostname !== "string" || hostname.length === 0) {
    throw new TypeError("noe.serve hostname must be a non-empty string");
  }
  const port = options.port === undefined ? 0 : options.port;
  if (typeof port !== "number" || !Number.isInteger(port) || port < 0 || port > 65535) {
    throw new RangeError("noe.serve port must be an integer from 0 to 65535");
  }
  return {hostname, port};
}`

const serveRequestFactory = `(fields, headerEntries, body, copyBytes) => {
  const headers = Object.create(null);
  for (const [name, values] of headerEntries) {
    Object.defineProperty(headers, name, {
      value: values, enumerable: true, writable: true, configurable: true
    });
  }
  return {
    method: fields.method,
    url: fields.url,
    path: fields.path,
    headers,
    text: async () => body,
    json: async () => JSON.parse(body),
    bytes: async () => copyBytes(),
  };
}`

type serveExchange struct {
	method  string
	url     string
	path    string
	headers map[string][]string
	body    []byte
	reply   chan serveReply
	// gone closes when the HTTP handler stops waiting, including client
	// disconnects. A late JS result is discarded instead of holding a Go
	// goroutine or writing to a disconnected client.
	gone chan struct{}
}

func (s *serveServer) handle(ex *serveExchange) {
	select {
	case <-ex.gone:
		return
	default:
	}

	fields := s.api.h.rt.NewObject()
	headerNames := make([]string, 0, len(ex.headers))
	for name := range ex.headers {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)
	headerEntries := make([]any, 0, len(headerNames))
	for _, name := range headerNames {
		headerEntries = append(headerEntries, []any{name, append([]string(nil), ex.headers[name]...)})
	}
	if err := fields.Set("method", ex.method); err != nil {
		s.failOrReply(ex, err)
		return
	}
	if err := fields.Set("url", ex.url); err != nil {
		s.failOrReply(ex, err)
		return
	}
	if err := fields.Set("path", ex.path); err != nil {
		s.failOrReply(ex, err)
		return
	}
	body := append([]byte(nil), ex.body...)
	request, err := s.api.requestFactory.Call(fields, headerEntries, string(body), func() quickjs.Value {
		return s.api.h.rt.NewBytes(body)
	})
	if err != nil {
		s.failOrReply(ex, err)
		return
	}
	result, err := s.handler.CallContext(s.source.ctx, request)
	if err != nil {
		if serveFatalError(err) {
			s.source.Fail(err)
			return
		}
		s.writeFailure(ex, err)
		return
	}

	fulfilled := func(value quickjs.Value) {
		select {
		case <-ex.gone:
			return
		default:
		}
		response, err := s.api.responseParser.Call(value)
		if err != nil {
			s.failOrReply(ex, err)
			return
		}
		normalized, err := parseServeResponse(response)
		if err != nil {
			s.failOrReply(ex, err)
			return
		}
		s.writeReply(ex, normalized)
	}
	rejected := func(reason quickjs.Value) {
		select {
		case <-ex.gone:
			return
		default:
		}
		s.writeFailure(ex, serveRejection(reason))
	}
	if _, err := s.api.observe.Call(result, fulfilled, rejected); err != nil {
		s.failOrReply(ex, err)
	}
}

func (s *serveServer) failOrReply(ex *serveExchange, err error) {
	if serveFatalError(err) {
		s.source.Fail(err)
		return
	}
	s.writeFailure(ex, err)
}

func (s *serveServer) writeReply(ex *serveExchange, reply serveReply) {
	select {
	case <-ex.gone:
		return
	default:
	}
	select {
	case ex.reply <- reply:
	case <-ex.gone:
	}
}

func (s *serveServer) writeFailure(ex *serveExchange, err error) {
	if err == nil {
		err = http.ErrAbortHandler
	}
	_, _ = fmt.Fprintln(s.api.stderr, "noe.serve handler: "+Diagnostic(err))
	s.writeReply(ex, serveReply{status: http.StatusInternalServerError, err: err})
}

func serveRequestURL(r *http.Request) string {
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	requestURI := r.URL.RequestURI()
	if requestURI == "*" {
		// The asterisk-form is valid for OPTIONS but cannot be appended as-is
		// to an absolute URL because that would produce "http://host*".
		requestURI = "/*"
	}
	return "http://" + host + requestURI
}

func serveRequestHeaders(r *http.Request) map[string][]string {
	headers := make(map[string][]string, len(r.Header)+1)
	names := make([]string, 0, len(r.Header))
	for name := range r.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		lower := strings.ToLower(name)
		headers[lower] = append([]string(nil), r.Header[name]...)
	}
	headers["host"] = []string{r.Host}
	return headers
}
