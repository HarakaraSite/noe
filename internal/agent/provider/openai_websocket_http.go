package provider

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
)

// WebSocketHTTPClient copies the borrowed client's policy and wraps only its
// RoundTripper to retain retry metadata from failed upgrade responses.
func (c *configuredHTTPClient) WebSocketHTTPClient() *http.Client {
	client, _ := borrowedWebSocketHTTPClient(c.client)
	if client == nil {
		return nil
	}
	copy := *client
	copy.Transport = &webSocketObservationTransport{base: client.Transport}
	return &copy
}

type webSocketObservationTransport struct{ base http.RoundTripper }

func (t *webSocketObservationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(request)
	if response == nil || response.StatusCode == http.StatusSwitchingProtocols {
		return response, err
	}
	state, _ := request.Context().Value(callContextKey{}).(*callState)
	if state != nil {
		code := ""
		if response.StatusCode >= http.StatusBadRequest && response.Body != nil {
			body := response.Body
			prefix, _ := io.ReadAll(io.LimitReader(body, maxProviderErrorCodeJSON))
			response.Body = &replayReadCloser{Reader: io.MultiReader(bytes.NewReader(prefix), body), Closer: body}
			code = providerErrorCodeFromJSON(prefix)
		}
		state.setResponseMetadata(response.StatusCode, response.Header, code)
	}
	return response, err
}

func (t *webSocketObservationTransport) CancelRequest(request *http.Request) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if canceler, ok := base.(interface{ CancelRequest(*http.Request) }); ok {
		canceler.CancelRequest(request)
	}
}

type replayReadCloser struct {
	io.Reader
	io.Closer
}

type borrowedWebSocketClientProvider interface {
	WebSocketHTTPClient() *http.Client
}

func borrowedWebSocketHTTPClient(client HTTPClient) (*http.Client, bool) {
	if client, ok := client.(*http.Client); ok {
		return client, true
	}
	provider, ok := client.(borrowedWebSocketClientProvider)
	if !ok {
		return nil, false
	}
	return provider.WebSocketHTTPClient(), true
}

func configureOpenAIWebSocketRequest(client *configuredHTTPClient, request *http.Request) (*http.Request, error) {
	request = request.Clone(request.Context())
	for key, value := range client.profile.Headers {
		request.Header.Set(key, value)
	}
	if state, _ := request.Context().Value(callContextKey{}).(*callState); state != nil {
		if client.profile.SessionHeader != "" {
			if state.sessionID == "" {
				return nil, errors.New("provider request requires a session ID")
			}
			request.Header.Set(client.profile.SessionHeader, state.sessionID)
		}
		endpoint, err := url.JoinPath(client.profile.BaseURL, client.profile.Path)
		if err != nil {
			return nil, err
		}
		request.URL, err = url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
	}
	return request, nil
}
