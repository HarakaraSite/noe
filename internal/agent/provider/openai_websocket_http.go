package provider

import (
	"errors"
	"net/http"
	"net/url"
)

// WebSocketHTTPClient forwards the borrowed transport's explicit upgrade
// capability. The SDK copies this client before installing its handshake
// wrapper, so its transport, redirect, timeout, Jar, and cancellation policies
// remain owned by the caller.
func (c *configuredHTTPClient) WebSocketHTTPClient() *http.Client {
	client, _ := borrowedWebSocketHTTPClient(c.client)
	return client
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
