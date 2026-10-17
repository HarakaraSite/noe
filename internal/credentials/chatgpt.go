// Package credentials owns ChatGPT token records independently of model SDKs.
package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DiscoveryURL = "https://auth.openai.com/.well-known/openid-configuration"
	Resource     = "https://api.openai.com/v1"
	PlanScope    = "chatgpt.tokens.use.direct"
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Options struct {
	HTTPClient   HTTPClient
	DiscoveryURL string // injectable discovery endpoint for local tests
}

// Record matches hjh's token record, so a selected account can be copied intact.
type Record struct {
	Issuer    string    `json:"issuer"`
	Subject   string    `json:"subject"`
	Email     string    `json:"email,omitempty"`
	ClientID  string    `json:"client_id"`
	HostID    string    `json:"ext_agent_host_id"`
	IDToken   string    `json:"id_token"`
	Access    string    `json:"access_token"`
	Refresh   string    `json:"refresh_token,omitempty"`
	TokenType string    `json:"token_type"`
	ExpiresAt time.Time `json:"expires_at"`
	Scopes    []string  `json:"scopes"`
	SavedAt   time.Time `json:"saved_at"`
}

type Tokens struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token"`
	IDToken   string `json:"id_token"`
	TokenType string `json:"token_type"`
	ExpiresIn int64  `json:"expires_in"`
	Scope     string `json:"scope"`
}

type discovery struct {
	Issuer        string `json:"issuer"`
	TokenEndpoint string `json:"token_endpoint"`
	JWKSURI       string `json:"jwks_uri"`
}

type ChatGPT struct {
	path   string
	opts   Options
	client HTTPClient
}

func NewChatGPT(path string, opts Options) *ChatGPT {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &ChatGPT{path: path, opts: opts, client: client}
}

func (c *ChatGPT) discover(ctx context.Context) (discovery, error) {
	endpoint := c.opts.DiscoveryURL
	if endpoint == "" {
		endpoint = DiscoveryURL
	}
	var d discovery
	if err := c.getJSON(ctx, endpoint, &d); err != nil {
		return d, err
	}
	if d.Issuer == "" || d.TokenEndpoint == "" || d.JWKSURI == "" {
		return d, errors.New("ChatGPT discovery is incomplete")
	}
	return d, nil
}

func (c *ChatGPT) getJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("invalid ChatGPT discovery URL")
	}
	res, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("ChatGPT discovery request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("ChatGPT discovery failed (HTTP %d)", res.StatusCode)
	}
	if json.NewDecoder(res.Body).Decode(target) != nil {
		return errors.New("invalid ChatGPT discovery response")
	}
	return nil
}

// Token serializes read/refresh/save across NOE processes using the same file.
// Refresh tokens rotate, so the latest record is read inside the file lock.
func (c *ChatGPT) Token(ctx context.Context) (string, error) {
	if c.path == "" {
		return "", errors.New("ChatGPT credential path is not configured")
	}
	var token string
	err := WithFileLock(ctx, c.path, func() error {
		record, err := Read(c.path)
		if err != nil {
			return err
		}
		if !hasScope(record.Scopes, PlanScope) {
			return errors.New("ChatGPT plan usage is not enabled")
		}
		if !record.ExpiresAt.After(time.Now().Add(30 * time.Second)) {
			if record.Refresh == "" {
				return errors.New("ChatGPT sign-in is required")
			}
			if err := c.refresh(ctx, &record); err != nil {
				return err
			}
			if err := Write(c.path, record); err != nil {
				return err
			}
		}
		if !hasScope(record.Scopes, PlanScope) {
			return errors.New("ChatGPT plan usage is not enabled")
		}
		token = record.Access
		return nil
	})
	return token, err
}

func (c *ChatGPT) refresh(ctx context.Context, record *Record) error {
	d, err := c.discover(ctx)
	if err != nil {
		return err
	}
	if d.Issuer != record.Issuer {
		return errors.New("ChatGPT credential issuer does not match")
	}
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {record.ClientID}, "refresh_token": {record.Refresh}, "resource": {Resource}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("invalid ChatGPT token endpoint")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("ChatGPT token refresh failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("ChatGPT token refresh failed (HTTP %d)", res.StatusCode)
	}
	var tokens Tokens
	if json.NewDecoder(res.Body).Decode(&tokens) != nil || tokens.Access == "" || tokens.Refresh == "" || tokens.ExpiresIn <= 0 {
		return errors.New("ChatGPT refresh response is incomplete")
	}
	record.Access, record.Refresh, record.TokenType = tokens.Access, tokens.Refresh, tokens.TokenType
	if tokens.IDToken != "" {
		record.IDToken = tokens.IDToken
	}
	record.SavedAt = time.Now().UTC()
	record.ExpiresAt = record.SavedAt.Add(time.Duration(tokens.ExpiresIn) * time.Second)
	if tokens.Scope != "" {
		record.Scopes = strings.Fields(tokens.Scope)
	}
	return nil
}

func hasScope(scopes []string, target string) bool {
	for _, scope := range scopes {
		if scope == target {
			return true
		}
	}
	return false
}

func Read(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, errors.New("could not read ChatGPT credentials; run auth-chatgpt.js")
	}
	var record Record
	if json.Unmarshal(data, &record) != nil || record.Issuer == "" || record.Subject == "" || record.ClientID == "" || record.Access == "" {
		return Record{}, errors.New("invalid ChatGPT credential record")
	}
	return record, nil
}

func Write(path string, record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return errors.New("could not encode ChatGPT credentials")
	}
	return WritePrivate(path, data)
}

func WritePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".noe-credential-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
