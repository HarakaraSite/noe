package jsonutil

import "encoding/json/jsontext"

// IsObject reports whether raw contains exactly one valid JSON object.
func IsObject(raw []byte) bool {
	value := jsontext.Value(raw)
	return value.Kind() == '{' && value.IsValid()
}
