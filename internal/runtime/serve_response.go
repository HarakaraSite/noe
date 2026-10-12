package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	quickjs "github.com/go-quickjs/go-quickjs"
)

const serveResponseFactory = `(() => {
  const typedArrayTag = Object.getOwnPropertyDescriptor(
    Object.getPrototypeOf(Uint8Array.prototype), Symbol.toStringTag
  ).get;
  const apply = Reflect.apply;
  return response => {
    if (response === null || typeof response !== "object" || Array.isArray(response)) {
      throw new TypeError("noe.serve handler must return a response object");
    }
    const status = response.status === undefined ? 200 : response.status;
    if (typeof status !== "number" || !Number.isInteger(status) || status < 200 || status > 599) {
      throw new RangeError("response status must be an integer from 200 to 599");
    }
    const headers = response.headers === undefined ? {} : response.headers;
    if (headers === null || typeof headers !== "object" || Array.isArray(headers)) {
      throw new TypeError("response headers must be an object");
    }
    const pairs = [];
    for (const name of Object.keys(headers)) {
      const value = headers[name];
      if (typeof value === "string") {
        pairs.push([name, value]);
      } else if (Array.isArray(value)) {
        for (const item of value) {
          if (typeof item !== "string") throw new TypeError("response header values must be strings");
          pairs.push([name, item]);
        }
      } else {
        throw new TypeError("response header values must be strings or arrays of strings");
      }
    }
    const body = response.body;
    if (body !== undefined && typeof body !== "string") {
      if (body === null || typeof body !== "object" || apply(typedArrayTag, body, []) !== "Uint8Array") {
        throw new TypeError("response body must be a string or Uint8Array");
      }
    }
    return {status, headers: pairs, body};
  };
})()`

const serveObserverFactory = `(() => {
  const P = Promise, resolve = P.resolve, then = P.prototype.then, apply = Reflect.apply;
  return (value, fulfilled, rejected) => {
    const promise = apply(resolve, P, [value]);
    apply(then, promise, [fulfilled, rejected]);
  };
})()`

type serveHeader struct {
	name  string
	value string
}

type serveReply struct {
	status  int
	headers []serveHeader
	body    []byte
	err     error
}

type serveResponseError struct{ message string }

func (e serveResponseError) Error() string { return e.message }

func invalidServeResponse(format string, args ...any) error {
	return serveResponseError{message: fmt.Sprintf(format, args...)}
}

func parseServeResponse(value quickjs.Value) (serveReply, error) {
	var out serveReply
	status, err := value.Get("status")
	if err != nil {
		return out, err
	}
	if status.Kind() != quickjs.KindNumber {
		return out, invalidServeResponse("normalized HTTP response has no numeric status")
	}
	out.status = status.Int()
	if out.status < http.StatusOK || out.status > 599 {
		return out, invalidServeResponse("normalized HTTP response status is outside 200..599: %d", out.status)
	}

	headers, err := value.Get("headers")
	if err != nil {
		return out, err
	}
	if !headers.IsArray() {
		return out, invalidServeResponse("normalized HTTP response headers are not an array")
	}
	for i := 0; i < headers.Len(); i++ {
		pair, err := headers.Index(i)
		if err != nil {
			return out, err
		}
		if !pair.IsArray() || pair.Len() != 2 {
			return out, invalidServeResponse("normalized HTTP response contains an invalid header")
		}
		nameValue, err := pair.Index(0)
		if err != nil {
			return out, err
		}
		valueValue, err := pair.Index(1)
		if err != nil {
			return out, err
		}
		if !nameValue.IsString() || !valueValue.IsString() {
			return out, invalidServeResponse("normalized HTTP response header is not a string pair")
		}
		name, headerValue := nameValue.String(), valueValue.String()
		if !validServeHeaderName(name) {
			return out, invalidServeResponse("invalid HTTP response header name %q", name)
		}
		if !validServeHeaderValue(headerValue) {
			return out, invalidServeResponse("invalid HTTP response header value for %q", name)
		}
		out.headers = append(out.headers, serveHeader{name: name, value: headerValue})
	}

	body, err := value.Get("body")
	if err != nil {
		return out, err
	}
	if body.IsUndefined() {
		return out, nil
	}
	if body.IsString() {
		out.body = []byte(body.String())
		return out, nil
	}
	data, ok := body.Bytes()
	if !ok {
		return out, invalidServeResponse("response Uint8Array is detached")
	}
	out.body = append([]byte(nil), data...)
	return out, nil
}

func validServeHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		default:
			return false
		}
	}
	return true
}

func validServeHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == 0x7f || (c < 0x20 && c != '\t') {
			return false
		}
	}
	return true
}

func serveFatalError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, quickjs.ErrClosed) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var responseErr serveResponseError
	if errors.As(err, &responseErr) {
		return false
	}
	var jsErr *quickjs.Error
	return !errors.As(err, &jsErr)
}

func serveRejection(reason quickjs.Value) error {
	if reason.IsObject() {
		if stack, err := reason.Get("stack"); err == nil && stack.IsString() && stack.String() != "" {
			return errors.New(stack.String())
		}
	}
	return fmt.Errorf("promise rejected: %s", reason.String())
}
