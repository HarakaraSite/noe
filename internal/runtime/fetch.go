package runtime

import (
	"context"
	"io"
	"net/http"
	"strings"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func (h *host) installFetch() error {
	send := func(url, method string, headers map[string]string, body string) quickjs.Value {
		return h.rt.Go(func(ctx context.Context) (any, error) {
			req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
			if err != nil {
				return nil, err
			}
			for key, value := range headers {
				req.Header.Set(key, value)
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return nil, err
			}
			defer res.Body.Close()
			data, err := io.ReadAll(res.Body)
			if err != nil {
				return nil, err
			}
			return map[string]any{"status": res.StatusCode, "body": string(data)}, nil
		})
	}
	factory, err := h.rt.EvalFile("<noe-fetch>", fetchFactory)
	if err != nil {
		return err
	}
	fetch, err := factory.Call(send)
	if err != nil {
		return err
	}
	return h.rt.Set("fetch", fetch)
}

// Validation and response methods stay on the runtime's goroutine; only Go
// strings, headers and the fully read body cross the HTTP goroutine boundary.
const fetchFactory = `(send => async function fetch(url, options = {}) {
  if (typeof url !== "string") throw new TypeError("fetch URL must be a string");
  if (options === null || typeof options !== "object" || Array.isArray(options)) {
    throw new TypeError("fetch options must be an object");
  }
  for (const name of Object.keys(options)) {
    if (name !== "method" && name !== "headers" && name !== "body") {
      throw new TypeError("unsupported fetch option: " + name);
    }
  }
  let method = options.method === undefined ? "GET" : options.method;
  if (typeof method !== "string") throw new TypeError("fetch method must be a string");
  method = method.toUpperCase();
  if (method !== "GET" && method !== "POST") throw new TypeError("fetch supports GET and POST only");
  const headers = options.headers === undefined ? {} : options.headers;
  if (headers === null || typeof headers !== "object" || Array.isArray(headers)) {
    throw new TypeError("fetch headers must be an object of strings");
  }
  const copiedHeaders = Object.create(null);
  for (const name of Object.keys(headers)) {
    if (typeof headers[name] !== "string") throw new TypeError("fetch header values must be strings");
    copiedHeaders[name] = headers[name];
  }
  const body = options.body;
  if (body !== undefined && (method !== "POST" || typeof body !== "string")) {
    throw new TypeError("fetch body must be a string used with POST");
  }
  const response = await send(url, method, copiedHeaders, body === undefined ? "" : body);
  const text = response.body;
  return {
    status: response.status,
    ok: response.status >= 200 && response.status < 300,
    text: async () => text,
    json: async () => JSON.parse(text),
  };
})`
