// Package jsonutil contains application-specific JSON v2 options.
package jsonutil

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
)

var preserveNumbers = json.WithUnmarshalers(json.UnmarshalFromFunc(func(dec *jsontext.Decoder, out *any) error {
	if dec.PeekKind() != '0' {
		return errors.ErrUnsupported
	}
	token, err := dec.ReadToken()
	if err != nil {
		return err
	}
	*out = jsonv1.Number(token.String())
	return nil
}))

// PreserveNumbers retains exact JSON number spellings in interface values.
// It is used where counters, budgets, or comparisons must not round through
// float64. All other JSON v2 behavior, including strict syntax, is unchanged.
func PreserveNumbers() json.Options { return preserveNumbers }
