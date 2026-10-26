package stdlib_test

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

type moduleRoundTripper struct {
	roundTrip func(*http.Request) (*http.Response, error)
	closed    atomic.Int32
}

func (r *moduleRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.roundTrip(req)
}
func (r *moduleRoundTripper) CloseIdleConnections() { r.closed.Add(1) }

type moduleTestBody struct {
	io.Reader
	closes atomic.Int32
}

func (b *moduleTestBody) Close() error { b.closes.Add(1); return nil }

func moduleTestLoader(t *testing.T, options *stdlib.ModuleHTTPOptions) quickjs.ModuleLoader {
	t.Helper()
	loader, err := stdlib.ModuleLoader(stdlib.LoaderOptions{HTTP: options}).NewLoader(t.Context(), stdlib.Origin{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return loader
}

func TestModuleHTTPValidatesResponsesAndClosesOwnedBodies(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
		limit                   int64
		want                    string
	}{
		{"plain BOM", "Text/Plain; charset=utf-8", "\xef\xbb\xbfexport const value=1;", 200, 0, ""},
		{"partial success", "application/javascript", "export {};", 206, 0, ""},
		{"missing MIME", "", "export {};", 200, 0, "invalid Content-Type"},
		{"invalid MIME", "text/plain; charset=\"", "export {};", 200, 0, "invalid Content-Type"},
		{"HTML not JS", "text/html", "export {};", 200, 0, "unsupported Content-Type"},
		{"non-success", "text/plain", "export {};", 503, 0, "HTTP status 503"},
		{"unresolved redirect", "text/plain", "export {};", 302, 0, "HTTP status 302"},
		{"invalid UTF-8", "text/plain", "export const v='\xff';", 200, 0, "not valid UTF-8"},
		{"body limit", "text/plain", "export {};", 200, 3, "body limit of 3"},
		{"exact limit", "text/plain", "export {};", 200, 10, ""},
		{"unlimited", "text/plain", "export {};", 200, -1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &moduleTestBody{Reader: strings.NewReader(tc.body)}
			transport := &moduleRoundTripper{roundTrip: func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{tc.contentType}}, Body: body, Request: req}, nil
			}}
			client := &http.Client{Transport: transport}
			loader := moduleTestLoader(t, &stdlib.ModuleHTTPOptions{Client: client, MaxBodyBytes: tc.limit})
			source, id, err := loader("https://example.invalid/module.js", "/entry.js")
			if tc.want == "" {
				if err != nil || source != strings.TrimPrefix(tc.body, "\xef\xbb\xbf") || id != "https://example.invalid/module.js" {
					t.Fatalf("source=%q id=%q error=%v", source, id, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "/entry.js") {
				t.Fatalf("error=%v want %q and referrer", err, tc.want)
			}
			if body.closes.Load() != 1 || transport.closed.Load() != 0 || client.Timeout != 0 {
				t.Fatalf("body closes=%d transport closes=%d timeout=%v", body.closes.Load(), transport.closed.Load(), client.Timeout)
			}
		})
	}
}

func TestModuleHTTPAcceptsJavaScriptMIMEContract(t *testing.T) {
	for _, media := range []string{
		"application/ecmascript", "application/javascript", "application/x-ecmascript", "application/x-javascript",
		"text/ecmascript", "text/javascript", "text/javascript1.0", "text/javascript1.1",
		"text/javascript1.2", "text/javascript1.3", "text/javascript1.4", "text/javascript1.5",
		"text/jscript", "text/livescript", "text/x-ecmascript", "text/x-javascript",
	} {
		t.Run(media, func(t *testing.T) {
			transport := &moduleRoundTripper{roundTrip: func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{media}}, Body: io.NopCloser(strings.NewReader("export {};")), Request: req}, nil
			}}
			if _, _, err := moduleTestLoader(t, &stdlib.ModuleHTTPOptions{Client: &http.Client{Transport: transport}})("https://example.invalid/m", "/entry.js"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestModuleHTTPDefaultAndDecompressedBodyLimit(t *testing.T) {
	t.Run("default 32MiB", func(t *testing.T) {
		body := &moduleTestBody{Reader: io.LimitReader(moduleSpaces{}, (32<<20)+1)}
		transport := &moduleRoundTripper{roundTrip: func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: body, Request: req}, nil
		}}
		_, _, err := moduleTestLoader(t, &stdlib.ModuleHTTPOptions{Client: &http.Client{Transport: transport}})("https://example.invalid/m", "/entry.js")
		if err == nil || !strings.Contains(err.Error(), "body limit of 33554432") || body.closes.Load() != 1 {
			t.Fatalf("default limit error=%v closes=%d", err, body.closes.Load())
		}
	})
	t.Run("decompressed bytes", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript")
			w.Header().Set("Content-Encoding", "gzip")
			z := gzip.NewWriter(w)
			_, _ = io.WriteString(z, strings.Repeat(" ", 256))
			_ = z.Close()
		}))
		defer server.Close()
		_, _, err := moduleTestLoader(t, &stdlib.ModuleHTTPOptions{Client: server.Client(), MaxBodyBytes: 64})(server.URL, "/entry.js")
		if err == nil || !strings.Contains(err.Error(), "body limit of 64") {
			t.Fatalf("decompressed limit error=%v", err)
		}
	})
}

type moduleSpaces struct{}

func (moduleSpaces) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

func TestModuleHTTPRedirectFragmentsAndBorrowedClient(t *testing.T) {
	for _, tc := range []struct{ location, fragment string }{
		{"/final", "old%2f"}, {"/final#new%2f", "new%2f"}, {"/final#", ""},
	} {
		t.Run(tc.location, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path == "/start" {
					// Write Location verbatim; an explicit empty # must survive.
					w.Header().Set("Location", tc.location)
					w.WriteHeader(302)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, "export {};")
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = 11 * time.Second
			var checked int
			hook := func(req *http.Request, via []*http.Request) error {
				checked++
				if req.URL.EscapedFragment() != tc.fragment {
					t.Errorf("redirect fragment=%q want %q", req.URL.EscapedFragment(), tc.fragment)
				}
				req.Header.Set("X-Host-Hook", "preserved")
				return nil
			}
			client.CheckRedirect = hook
			transport := client.Transport
			var allowed []string
			loader := moduleTestLoader(t, &stdlib.ModuleHTTPOptions{Client: client, Allow: func(req *http.Request) error {
				allowed = append(allowed, req.URL.String())
				if req.URL.Path == "/final" && req.Header.Get("X-Host-Hook") != "preserved" {
					t.Error("host CheckRedirect not respected")
				}
				return nil
			}})
			_, id, err := loader(server.URL+"/start#old%2f", "/entry.js")
			want := server.URL + "/final"
			if tc.fragment != "" {
				want += "#" + tc.fragment
			}
			if err != nil || id != want || checked != 1 || len(allowed) != 2 || requests.Load() != 2 {
				t.Fatalf("id=%q want=%q err=%v checked=%d allowed=%v requests=%d", id, want, err, checked, allowed, requests.Load())
			}
			if client.Timeout != 11*time.Second || client.Transport != transport || reflect.ValueOf(client.CheckRedirect).Pointer() != reflect.ValueOf(hook).Pointer() {
				t.Fatal("borrowed client was changed")
			}
			response, err := client.Get(server.URL + "/final")
			if err != nil {
				t.Fatal("borrowed client unusable:", err)
			}
			_ = response.Body.Close()
		})
	}
}

func TestModuleHTTPPermissionAndRedirectRejection(t *testing.T) {
	var finalRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			w.Header().Set("Location", "/final")
			w.WriteHeader(302)
		case "/scheme":
			w.Header().Set("Location", "file:///tmp/m.js")
			w.WriteHeader(302)
		default:
			finalRequests.Add(1)
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "export {};")
		}
	}))
	defer server.Close()
	denied := errors.New("host denied module")
	for _, tc := range []struct {
		name, path string
		allow      func(*http.Request) error
		check      func(*http.Request, []*http.Request) error
		want       string
		cause      error
	}{
		{"initial deny", "/final", func(*http.Request) error { return denied }, nil, "host denied module", denied},
		{"redirect deny", "/start", func(r *http.Request) error {
			if r.URL.Path == "/final" {
				return denied
			}
			return nil
		}, nil, "host denied module", denied},
		{"host hook deny", "/start", nil, func(*http.Request, []*http.Request) error { return denied }, "host denied module", denied},
		{"host keeps response", "/start", nil, func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, "HTTP status 302", nil},
		{"non HTTP redirect", "/scheme", nil, nil, "HTTP/HTTPS", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := *server.Client()
			client.CheckRedirect = tc.check
			_, _, err := moduleTestLoader(t, &stdlib.ModuleHTTPOptions{Client: &client, Allow: tc.allow})(server.URL+tc.path, "/entry.js")
			if err == nil || !strings.Contains(err.Error(), tc.want) || (tc.cause != nil && !errors.Is(err, tc.cause)) {
				t.Fatalf("error=%v want %q cause=%v", err, tc.want, tc.cause)
			}
		})
	}
	if finalRequests.Load() != 0 {
		t.Fatalf("denied requests reached target: %d", finalRequests.Load())
	}
}

func TestModuleHTTPReadFailuresAreNotRetained(t *testing.T) {
	sentinel := errors.New("body read failed")
	body := &moduleTestBody{Reader: moduleErrorReader{sentinel}}
	var requests atomic.Int32
	transport := &moduleRoundTripper{roundTrip: func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		var reader io.ReadCloser = body
		if requests.Load() > 1 {
			reader = io.NopCloser(strings.NewReader("export {};"))
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: reader, Request: req}, nil
	}}
	loader := moduleTestLoader(t, &stdlib.ModuleHTTPOptions{Client: &http.Client{Transport: transport}})
	if _, _, err := loader("https://example.invalid/m", "/entry.js"); !errors.Is(err, sentinel) || body.closes.Load() != 1 {
		t.Fatalf("read failure=%v closes=%d", err, body.closes.Load())
	}
	if source, _, err := loader("https://example.invalid/m", "/entry.js"); err != nil || source != "export {};" || requests.Load() != 2 {
		t.Fatalf("retry after read failure source=%q err=%v requests=%d", source, err, requests.Load())
	}
}

type moduleErrorReader struct{ err error }

func (r moduleErrorReader) Read([]byte) (int, error) { return 0, r.err }
