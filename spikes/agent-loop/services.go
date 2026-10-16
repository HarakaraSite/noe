package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// The upstream fixture requires a separate credential known only to Go. It
// exercises the same boundary a future search-provider adapter would use.
func (h *host) searchFixture(ctx context.Context, query string) (any, error) {
	body, _ := json.Marshal(map[string]string{"query": query})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url+"/fixture/search", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer "+h.serviceToken)
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("fixture status %d", response.StatusCode)
	}
	var value any
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func (h *host) serveSearchFixture(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.serviceToken)) != 1 {
		writeError(w, 401, "authentication", "fixture credential required")
		return
	}
	var body struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "request", err.Error())
		return
	}
	writeJSON(w, map[string]any{"source": "local authenticated fixture (not web search)", "query": body.Query, "results": []any{map[string]string{"title": "NOE spike fixture", "snippet": "The fixture answer is 42."}}})
}
