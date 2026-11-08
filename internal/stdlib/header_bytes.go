package stdlib

import (
	"errors"
	"strings"
)

// HTTP fields carry bytes. JavaScript ByteStrings represent each byte with the
// same code point, independently of whether the wire bytes form valid UTF-8.
func headerString(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		out.WriteRune(rune(value[i]))
	}
	return out.String()
}

func headerBytes(value string) (string, error) {
	out := make([]byte, 0, len(value))
	for _, c := range value {
		if c > 255 {
			return "", errors.New("HTTP header value must be a ByteString")
		}
		out = append(out, byte(c))
	}
	return string(out), nil
}
