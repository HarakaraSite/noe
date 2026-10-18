package agent

import (
	"crypto/rand"
	"encoding/hex"
)

// randomID returns an unpredictable lowercase hexadecimal identifier.
func randomID() (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
