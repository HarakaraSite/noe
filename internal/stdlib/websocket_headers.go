package stdlib

import (
	"fmt"
	"net/http"
	"strings"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// readWebSocketHeaders copies normalized pairs before any socket or loop work
// is allocated. The returned header map belongs to this single connection.
func readWebSocketHeaders(pairs quickjs.Value) (http.Header, error) {
	if !pairs.IsArray() {
		return nil, fmt.Errorf("WebSocket headers must be pairs")
	}
	headers := make(http.Header)
	for i := 0; i < pairs.Len(); i++ {
		pair, err := pairs.Index(i)
		if err != nil {
			return nil, err
		}
		if !pair.IsArray() || pair.Len() != 2 {
			return nil, fmt.Errorf("WebSocket header entries must be pairs")
		}
		name, err := pair.Index(0)
		if err != nil {
			return nil, err
		}
		value, err := pair.Index(1)
		if err != nil {
			return nil, err
		}
		if !name.IsString() || !value.IsString() {
			return nil, fmt.Errorf("WebSocket header entries must be strings")
		}
		key, field := name.String(), value.String()
		if !webSocketHeaderToken(key) {
			return nil, fmt.Errorf("invalid WebSocket header name")
		}
		switch strings.ToLower(key) {
		case "host", "connection", "upgrade", "sec-websocket-key", "sec-websocket-version",
			"sec-websocket-protocol", "sec-websocket-extensions", "content-length", "transfer-encoding", "trailer":
			return nil, fmt.Errorf("WebSocket header %q is managed by the connection", key)
		}
		for j := 0; j < len(field); j++ {
			if field[j] < 0x20 && field[j] != '\t' || field[j] == 0x7f {
				return nil, fmt.Errorf("invalid value for WebSocket header %q", key)
			}
		}
		headers.Add(key, strings.Trim(field, " \t"))
	}
	return headers, nil
}

func webSocketHeaderToken(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			continue
		}
		return false
	}
	return true
}
