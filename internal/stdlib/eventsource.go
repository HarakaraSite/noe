package stdlib

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// eventSourcePolicyError distinguishes a refused request from a transient
// transport failure. Retrying cannot make a host capability refusal succeed.
type eventSourcePolicyError struct{ error }

func (e eventSourcePolicyError) Unwrap() error { return e.error }

// eventSources shares Fetch's authority and transport, but not its cumulative
// response-body limit. An event stream can remain open indefinitely; the JS
// parser bounds the partial event instead. A caller-supplied client retains its
// timeout, while the default client has no whole-connection deadline.
func eventSources(rt *quickjs.Runtime, cfg *Fetch) error {
	// Low-level Network can also be installed without WebAPIs or Timers. Keep
	// that existing mode usable rather than adding implicit capabilities.
	for _, name := range []string{"EventTarget", "Event", "MessageEvent", "URL", "TextDecoder", "TextEncoder", "setTimeout", "clearTimeout"} {
		value, err := rt.Get(name)
		if err != nil {
			return err
		}
		if !value.IsFunction() {
			return nil
		}
	}
	options := *cfg
	if cfg.Allow != nil {
		options.Allow = func(req *http.Request) error {
			if err := cfg.Allow(req); err != nil {
				return eventSourcePolicyError{err}
			}
			return nil
		}
	}
	client := http.Client{}
	if cfg.Client != nil {
		client = *cfg.Client
	}
	if next := client.CheckRedirect; next != nil {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if err := next(req, via); err != nil && !errors.Is(err, http.ErrUseLastResponse) {
				return eventSourcePolicyError{err}
			} else {
				return err
			}
		}
	}
	authenticated := &client
	if options.Allow != nil {
		authenticated = allowRedirects(authenticated, options.Allow)
	}
	withoutCookies := *authenticated
	withoutCookies.Jar = nil
	limit := cfg.MaxBodyBytes
	if limit == 0 {
		limit = 32 << 20
	}
	loop := cfg.Loop
	host := rt.NewObject()
	if err := setAll(host, map[string]any{
		"maxEventBytes": limit,
		"nextTask": func() *quickjs.Promise {
			p := rt.NewPromise()
			loop.postExecution(func() { p.Resolve(nil) })
			return p
		},
		"connect": func(rawURL string, credentials bool, lastID string, register quickjs.Value) *quickjs.Promise {
			p := rt.NewPromise()
			target, err := url.Parse(rawURL)
			if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
				rejectEventSource(rt, p, eventSourcePolicyError{errors.New("EventSource needs an HTTP or HTTPS URL")})
				return p
			}
			target.Fragment = ""
			spec := &requestSpec{method: "GET", url: target.String(), headers: [][2]string{
				{"Accept", "text/event-stream"},
				{"Cache-Control", "no-cache"},
			}}
			if lastID != "" {
				spec.headers = append(spec.headers, [2]string{"Last-Event-ID", lastID})
			}
			spec.ctx, spec.cancel = context.WithCancel(loopContext(loop))
			if _, err := register.Call(spec.cancel); err != nil {
				spec.cancel()
				p.RejectError(err)
				return p
			}
			selected := authenticated
			if !credentials {
				selected = &withoutCookies
			}
			loop.Begin()
			if err := loop.Go(func(context.Context) {
				defer loop.Done()
				res, err := doFetch(selected, &options, spec, -1, loop)
				var removeCleanup func()
				if res != nil && res.stream != nil {
					if scope := loop.executionScope(); scope != nil {
						removeCleanup, err = scope.AddCleanupHandle(res.stream.finish)
						if err != nil {
							res.stream.finish()
							return
						}
					}
				}
				loop.postExecution(func() {
					if res != nil && res.stream != nil {
						res.stream.scopeUnhook = removeCleanup
					}
					if err != nil {
						rejectEventSource(rt, p, err)
						return
					}
					deliver(rt, p, res, nil)
				})
			}); err != nil {
				loop.Done()
				spec.cancel()
				rejectEventSource(rt, p, err)
			}
			return p
		},
	}); err != nil {
		return err
	}
	api, err := evalWithHost(rt, "<eventsource>", eventSourceJS, host)
	if err != nil {
		return err
	}
	constructor, err := api.Get("EventSource")
	if err != nil {
		return err
	}
	return rt.Set("EventSource", constructor)
}

func rejectEventSource(rt *quickjs.Runtime, p *quickjs.Promise, err error) {
	e := rt.NewError("TypeError", "EventSource connection failed")
	var denied eventSourcePolicyError
	if errors.As(err, &denied) {
		e.Set("eventSourceFatal", true)
	}
	p.Reject(e)
}
