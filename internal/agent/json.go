package agent

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
)

// validJSON reports whether raw contains one complete JSON value using the
// standard library's native v2 syntax rules.
func validJSON(raw []byte) bool {
	return jsontext.Value(raw).IsValid()
}

type jsonByteCount int

func (n *jsonByteCount) Write(p []byte) (int, error) {
	*n += jsonByteCount(len(p))
	return len(p), nil
}

// jsonEncodedSize measures the native v2 representation, streaming ordinary
// histories instead of retaining a complete encoded copy. Validation and
// escaping use the standard encoder.
func jsonEncodedSize(values []jsonv1.RawMessage) (int, error) {
	if largeJSONValues(values) {
		encoded, err := json.Marshal(values)
		return len(encoded), err
	}
	var n jsonByteCount
	err := json.MarshalWrite(&n, values)
	return int(n), err
}

// Go 1.27's streaming encoder discards buffers above 64 KiB. Keep buffered
// encoding for large raw values to avoid reallocating their encoding on each
// context pass. This is an allocation heuristic, not an input-size limit.
func largeJSONValues(groups ...[]jsonv1.RawMessage) bool {
	for _, values := range groups {
		for _, value := range values {
			if len(value) > 64<<10 {
				return true
			}
		}
	}
	return false
}
