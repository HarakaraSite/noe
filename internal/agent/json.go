package agent

import "encoding/json/jsontext"

// validJSON reports whether raw contains one complete JSON value using the
// standard library's native v2 syntax rules.
func validJSON(raw []byte) bool {
	return jsontext.Value(raw).IsValid()
}
