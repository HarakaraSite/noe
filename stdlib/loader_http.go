package stdlib

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"
)

const defaultModuleBodyBytes int64 = 32 << 20

func httpModuleReader(ctx context.Context, options ModuleHTTPOptions) modulemap.RemoteReader {
	return func(id string, retained func(string) bool) (string, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, id, nil)
		if err != nil {
			return "", "", err
		}
		if err := allowModuleRequest(req, options.Allow); err != nil {
			return "", "", err
		}
		// Copy the client for this request, never changing a borrowed client's
		// Timeout, Transport, Jar or CheckRedirect and never closing it.
		client := http.Client{Timeout: 30 * time.Second}
		if options.Client != nil {
			client = *options.Client
		}
		checkRedirect := client.CheckRedirect
		current := req
		client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			location := next.Response.Header.Get("Location")
			if strings.Contains(location, "#") {
				u, err := url.Parse(location)
				if err != nil {
					return err
				}
				next.URL.Fragment, next.URL.RawFragment = u.Fragment, u.RawFragment
			} else {
				previous := via[len(via)-1].URL
				next.URL.Fragment, next.URL.RawFragment = previous.Fragment, previous.RawFragment
			}
			if checkRedirect != nil {
				if err := checkRedirect(next, via); err != nil {
					return err
				}
			} else if len(via) >= 10 {
				// Preserve net/http's default when installing our own hook.
				return fmt.Errorf("stopped after 10 redirects")
			}
			if err := allowModuleRequest(next, options.Allow); err != nil {
				return err
			}
			current = next
			return nil
		}
		response, err := client.Do(req)
		if err != nil {
			// net/http closes response bodies on errors, including rejected
			// redirects; ErrUseLastResponse instead returns a usable response.
			return "", "", fmt.Errorf("GET %q: %w", id, err)
		}
		defer response.Body.Close()
		finalID, err := modulemap.CanonicalHTTPURL(current.URL.String())
		if err != nil {
			return "", "", err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return "", "", fmt.Errorf("GET %q: HTTP status %d", finalID, response.StatusCode)
		}
		// A new redirect alias may reach a module already loaded in this VM.
		// Its original source wins; no replacement body is needed. The new
		// response body is still always closed.
		if retained(finalID) {
			return "", finalID, nil
		}
		contentType := response.Header.Get("Content-Type")
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			return "", "", fmt.Errorf("module %q: invalid Content-Type %q: %w", finalID, contentType, err)
		}
		if !moduleMediaType(strings.ToLower(mediaType)) {
			return "", "", fmt.Errorf("module %q: unsupported Content-Type %q", finalID, contentType)
		}
		maxBytes := options.MaxBodyBytes
		if maxBytes == 0 {
			maxBytes = defaultModuleBodyBytes
		}
		var reader io.Reader = response.Body
		if maxBytes >= 0 && maxBytes < math.MaxInt64 {
			reader = io.LimitReader(reader, maxBytes+1)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return "", "", fmt.Errorf("read module %q: %w", finalID, err)
		}
		if maxBytes >= 0 && int64(len(data)) > maxBytes {
			return "", "", fmt.Errorf("module %q exceeds body limit of %d bytes", finalID, maxBytes)
		}
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		if !utf8.Valid(data) {
			return "", "", fmt.Errorf("module %q: source is not valid UTF-8", finalID)
		}
		return string(data), finalID, nil
	}
}

func allowModuleRequest(req *http.Request, allow func(*http.Request) error) error {
	if err := canonicalModuleRequest(req); err != nil {
		return err
	}
	if allow != nil {
		if err := allow(req); err != nil {
			return fmt.Errorf("module URL %q denied: %w", req.URL.String(), err)
		}
		// Host callbacks may edit headers or the URL. Even then this reader
		// only issues hierarchical HTTP/HTTPS requests.
		return canonicalModuleRequest(req)
	}
	return nil
}

func canonicalModuleRequest(req *http.Request) error {
	id, err := modulemap.CanonicalHTTPURL(req.URL.String())
	if err != nil {
		return err
	}
	req.URL, err = url.Parse(id)
	return err
}

func moduleMediaType(value string) bool {
	switch value {
	case "application/ecmascript", "application/javascript",
		"application/x-ecmascript", "application/x-javascript",
		"text/ecmascript", "text/javascript",
		"text/javascript1.0", "text/javascript1.1", "text/javascript1.2",
		"text/javascript1.3", "text/javascript1.4", "text/javascript1.5",
		"text/jscript", "text/livescript", "text/x-ecmascript",
		"text/x-javascript", "text/plain":
		return true
	default:
		return false
	}
}
