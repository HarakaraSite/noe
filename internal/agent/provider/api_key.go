package provider

import (
	"errors"
	"strings"
	"sync"
)

// apiKeyFile is shared credential I/O, independent of either provider SDK.
type apiKeyFile struct {
	keyFile  string
	readFile func(string) ([]byte, error)
	keyMu    sync.Mutex
	key      string
}

func (p *apiKeyFile) apiKey() (string, error) {
	p.keyMu.Lock()
	defer p.keyMu.Unlock()
	if p.key != "" {
		return p.key, nil
	}
	if p.keyFile == "" {
		return "", errors.New("credential path is not configured")
	}
	contents, err := p.readFile(p.keyFile)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(contents))
	if key == "" {
		return "", errors.New("credential file is empty")
	}
	p.key = key
	return p.key, nil
}
