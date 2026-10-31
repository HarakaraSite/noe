package credentials

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	CodexImagesManaged        = "managed"
	CodexImagesExternalRO     = "external-readonly"
	codexImagesRecordSchema   = 1
	codexImagesCredentialKind = "noe-codex-images"
	codexRefreshMargin        = 30 * time.Second
	codexOAuthScope           = "openid profile email offline_access api.connectors.read api.connectors.invoke"
)

// CodexSnapshot binds one outbound image request to an immutable credential
// view. Token-bearing fields are deliberately excluded from JSON encoding.
type CodexSnapshot struct {
	Access    string   `json:"-"`
	AccountID string   `json:"-"`
	Secrets   []string `json:"-"`

	mode       string
	path       string
	identity   codexIdentity
	generation uint64
	version    string
	expiresAt  time.Time
}

type codexIdentity struct {
	issuer   string
	clientID string
	subject  string
	account  string
}

type codexExternalJWTIdentity struct {
	Subject string `json:"sub"`
	Auth    struct {
		ChatGPTUserID string `json:"chatgpt_user_id"`
		UserID        string `json:"user_id"`
	} `json:"https://api.openai.com/auth"`
}

type codexImagesRecord struct {
	SchemaVersion int       `json:"schema_version"`
	Kind          string    `json:"kind"`
	Issuer        string    `json:"issuer"`
	ClientID      string    `json:"client_id"`
	Subject       string    `json:"subject"`
	AccountID     string    `json:"account_id"`
	Email         string    `json:"email,omitempty"`
	AccessToken   string    `json:"access_token"`
	RefreshToken  string    `json:"refresh_token"`
	IDToken       string    `json:"id_token,omitempty"`
	ExpiresAt     time.Time `json:"expires_at"`
	SavedAt       time.Time `json:"saved_at"`
	Generation    uint64    `json:"generation"`
}

// CodexImages reads one explicitly selected credential file. Construction is
// lazy: no file or network access occurs until Snapshot or recovery is called.
type CodexImages struct {
	path   string
	mode   string
	opts   Options
	client HTTPClient
}

func NewCodexImages(path, mode string, opts Options) *CodexImages {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &CodexImages{path: path, mode: mode, opts: opts, client: client}
}

func (c *CodexImages) Snapshot(ctx context.Context) (CodexSnapshot, error) {
	if ctx == nil {
		return CodexSnapshot{}, errors.New("Codex image credentials require a context")
	}
	if err := ctx.Err(); err != nil {
		return CodexSnapshot{}, err
	}
	switch c.mode {
	case CodexImagesManaged:
		if c.path == "" {
			return CodexSnapshot{}, errors.New("managed Codex credential file is not configured")
		}
		var snapshot CodexSnapshot
		err := WithFileLock(ctx, c.path, func() error {
			record, err := readCodexImagesRecord(c.path, c.expectedIdentity())
			if err != nil {
				return err
			}
			if !record.ExpiresAt.After(time.Now().Add(codexRefreshMargin)) {
				if err := c.refresh(ctx, &record, "proactive_expiry"); err != nil {
					return err
				}
				if err := writeCodexImagesRecord(c.path, record); err != nil {
					return errors.New("could not save refreshed Codex image credentials")
				}
			}
			snapshot = snapshotFromRecord(c.path, c.mode, record)
			return nil
		})
		return snapshot, err
	case CodexImagesExternalRO:
		return c.externalSnapshot(ctx)
	default:
		return CodexSnapshot{}, errors.New("Codex image authentication mode must be managed or external-readonly")
	}
}

func (c *CodexImages) RecoverUnauthorized(ctx context.Context, rejected CodexSnapshot) (CodexSnapshot, error) {
	if ctx == nil {
		return CodexSnapshot{}, errors.New("Codex image credentials require a context")
	}
	if err := ctx.Err(); err != nil {
		return CodexSnapshot{}, err
	}
	if rejected.Access == "" || rejected.mode != c.mode || rejected.path != c.path || rejected.version == "" {
		return CodexSnapshot{}, errors.New("invalid rejected Codex image credential snapshot")
	}
	switch c.mode {
	case CodexImagesManaged:
		if c.path == "" {
			return CodexSnapshot{}, errors.New("managed Codex credential file is not configured")
		}
		var snapshot CodexSnapshot
		err := WithFileLock(ctx, c.path, func() error {
			record, err := readCodexImagesRecord(c.path, c.expectedIdentity())
			if err != nil {
				return err
			}
			identity := identityFromRecord(record)
			if !sameCodexIdentity(identity, rejected.identity) {
				return errors.New("Codex image credentials changed identity; sign in again with the original account")
			}
			newer := record.Generation != rejected.generation || recordVersion(record) != rejected.version
			if newer && record.ExpiresAt.After(time.Now().Add(codexRefreshMargin)) {
				snapshot = snapshotFromRecord(c.path, c.mode, record)
			} else {
				// An unchanged generation receiving 401 is forced through OAuth,
				// even while its locally recorded expiry is still in the future.
				if record.RefreshToken == "" {
					return errors.New("Codex image sign-in is required; run auth-codex-images.js")
				}
				if err := c.refresh(ctx, &record, "unauthorized_recovery"); err != nil {
					return err
				}
				if record.AccessToken == rejected.Access {
					return errors.New("Codex image authentication recovery did not produce a new access token")
				}
				if err := writeCodexImagesRecord(c.path, record); err != nil {
					return errors.New("could not save refreshed Codex image credentials")
				}
				snapshot = snapshotFromRecord(c.path, c.mode, record)
			}
			if snapshot.Access == rejected.Access {
				return errors.New("Codex image authentication recovery did not produce a new access token")
			}
			return nil
		})
		return snapshot, err
	case CodexImagesExternalRO:
		current, err := c.externalSnapshot(ctx)
		if err != nil {
			return CodexSnapshot{}, err
		}
		if !sameCodexIdentity(current.identity, rejected.identity) {
			return CodexSnapshot{}, errors.New("selected Codex account changed; select the original account before retrying")
		}
		if current.Access == rejected.Access {
			return CodexSnapshot{}, errors.New("external Codex credentials have not changed since the unauthorized response")
		}
		return current, nil
	default:
		return CodexSnapshot{}, errors.New("Codex image authentication mode must be managed or external-readonly")
	}
}

func (c *CodexImages) externalSnapshot(ctx context.Context) (CodexSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return CodexSnapshot{}, err
	}
	if c.path == "" {
		return CodexSnapshot{}, errors.New("Codex credential file is not configured")
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return CodexSnapshot{}, errors.New("could not read the selected Codex credential file")
	}
	var external struct {
		AuthMode     json.RawMessage `json:"auth_mode"`
		OpenAIAPIKey string          `json:"OPENAI_API_KEY"`
		Tokens       struct {
			AccessToken  string          `json:"access_token"`
			RefreshToken string          `json:"refresh_token"`
			IDToken      json.RawMessage `json:"id_token"`
			AccountID    string          `json:"account_id"`
			Provenance   struct {
				Issuer   string `json:"issuer"`
				ClientID string `json:"client_id"`
			} `json:"oauth_provenance"`
		} `json:"tokens"`
	}
	if json.Unmarshal(data, &external) != nil {
		return CodexSnapshot{}, errors.New("selected file is not a valid Codex credential record")
	}
	mode := ""
	if len(external.AuthMode) != 0 && string(external.AuthMode) != "null" {
		if json.Unmarshal(external.AuthMode, &mode) != nil {
			return CodexSnapshot{}, errors.New("selected file does not contain Codex ChatGPT credentials")
		}
	}
	if external.OpenAIAPIKey != "" || (mode != "" && mode != "chatgpt" && mode != "chatgptAuthTokens") {
		return CodexSnapshot{}, errors.New("selected file does not contain Codex ChatGPT credentials")
	}
	access, accountID := strings.TrimSpace(external.Tokens.AccessToken), strings.TrimSpace(external.Tokens.AccountID)
	if access == "" || accountID == "" {
		return CodexSnapshot{}, errors.New("selected Codex ChatGPT credentials require an access token and account ID")
	}
	expiresAt, ok := tokenExpiry(access)
	if !ok {
		return CodexSnapshot{}, errors.New("selected Codex access token has no usable expiry")
	}
	if !expiresAt.After(time.Now().Add(codexRefreshMargin)) {
		return CodexSnapshot{}, errors.New("selected Codex access token is expired; refresh it through its owning login")
	}
	idToken := rawTokenString(external.Tokens.IDToken)
	identity := codexIdentity{
		issuer: external.Tokens.Provenance.Issuer, clientID: external.Tokens.Provenance.ClientID,
		subject: externalUserIdentity(idToken, access), account: accountID,
	}
	return CodexSnapshot{
		Access: access, AccountID: accountID,
		Secrets: nonEmptySecrets(access, external.Tokens.RefreshToken, idToken),
		mode:    c.mode, path: c.path, identity: identity,
		version: tokenVersion(access, external.Tokens.RefreshToken), expiresAt: expiresAt,
	}, nil
}

func (c *CodexImages) expectedIdentity() codexIdentity {
	return codexIdentity{issuer: codexIssuer(c.opts), clientID: codexClientID(c.opts)}
}

func (c *CodexImages) refresh(ctx context.Context, record *codexImagesRecord, reason string) error {
	if record.RefreshToken == "" {
		return errors.New("Codex image sign-in is required; run auth-codex-images.js")
	}
	d, err := discoverCodex(ctx, c.opts, c.client)
	if err != nil || d.Issuer != record.Issuer || !sameOrigin(record.Issuer, d.TokenEndpoint) {
		return errors.New("trusted Codex sign-in service discovery failed")
	}
	body, err := json.Marshal(map[string]string{
		"grant_type":     "refresh_token",
		"client_id":      record.ClientID,
		"refresh_token":  record.RefreshToken,
		"refresh_reason": reason,
	})
	if err != nil {
		return errors.New("could not prepare Codex token refresh")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(string(body)))
	if err != nil {
		return errors.New("invalid trusted Codex token endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "noe")
	req.Header.Set("originator", "noe")
	res, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Codex image token refresh failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Codex image token refresh failed (HTTP %d)", res.StatusCode)
	}
	var tokens codexTokenResponse
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&tokens) != nil || tokens.AccessToken == "" {
		return errors.New("Codex image token refresh response is incomplete")
	}
	expires, ok := responseExpiry(tokens)
	if !ok || !expires.After(time.Now().Add(codexRefreshMargin)) {
		return errors.New("Codex image token refresh returned an unusable access token")
	}
	record.AccessToken = tokens.AccessToken
	if tokens.RefreshToken != "" {
		record.RefreshToken = tokens.RefreshToken
	}
	if tokens.IDToken != "" {
		record.IDToken = tokens.IDToken
	}
	record.ExpiresAt = expires
	record.SavedAt = time.Now().UTC()
	record.Generation++
	return nil
}

func discoverCodex(ctx context.Context, opts Options, client HTTPClient) (discovery, error) {
	if !validCodexIssuer(codexIssuer(opts)) {
		return discovery{}, errors.New("configured Codex issuer is not trusted")
	}
	endpoint := strings.TrimSpace(opts.DiscoveryURL)
	if endpoint == "" {
		endpoint = codexIssuer(opts) + "/.well-known/openid-configuration"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return discovery{}, errors.New("invalid trusted Codex discovery URL")
	}
	req.Header.Set("User-Agent", "noe")
	req.Header.Set("originator", "noe")
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return discovery{}, ctx.Err()
		}
		return discovery{}, errors.New("trusted Codex sign-in service discovery failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return discovery{}, errors.New("trusted Codex sign-in service discovery failed")
	}
	var result discovery
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&result) != nil || result.Issuer != codexIssuer(opts) || result.TokenEndpoint == "" || result.JWKSURI == "" {
		return discovery{}, errors.New("trusted Codex sign-in service discovery failed")
	}
	if !sameOrigin(result.Issuer, result.TokenEndpoint) || !sameOrigin(result.Issuer, result.JWKSURI) {
		return discovery{}, errors.New("Codex sign-in discovery returned an untrusted endpoint")
	}
	return result, nil
}

type codexTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func writeCodexImagesRecord(path string, record codexImagesRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return errors.New("could not encode managed Codex image credentials")
	}
	if err := WritePrivate(path, data); err != nil {
		return errors.New("could not save managed Codex image credentials")
	}
	return nil
}

func readCodexImagesRecord(path string, expected codexIdentity) (codexImagesRecord, error) {
	if path == "" {
		return codexImagesRecord{}, errors.New("managed Codex credential file is not configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return codexImagesRecord{}, errors.New("could not read managed Codex image credentials; run auth-codex-images.js")
	}
	var record codexImagesRecord
	if json.Unmarshal(data, &record) != nil || record.SchemaVersion != codexImagesRecordSchema || record.Kind != codexImagesCredentialKind || record.Issuer == "" || record.ClientID == "" || record.Subject == "" || record.AccountID == "" || record.AccessToken == "" || record.RefreshToken == "" || record.ExpiresAt.IsZero() || record.Generation == 0 {
		return codexImagesRecord{}, errors.New("invalid managed Codex image credential record; run auth-codex-images.js")
	}
	if !validCodexIssuer(record.Issuer) || expected.issuer != record.Issuer || expected.clientID != record.ClientID {
		return codexImagesRecord{}, errors.New("managed Codex credentials do not match the configured issuer and client")
	}
	return record, nil
}

func snapshotFromRecord(path, mode string, record codexImagesRecord) CodexSnapshot {
	return CodexSnapshot{
		Access: record.AccessToken, AccountID: record.AccountID,
		Secrets: nonEmptySecrets(record.AccessToken, record.RefreshToken, record.IDToken),
		mode:    mode, path: path, identity: identityFromRecord(record),
		generation: record.Generation, version: recordVersion(record), expiresAt: record.ExpiresAt,
	}
}

func identityFromRecord(record codexImagesRecord) codexIdentity {
	return codexIdentity{issuer: record.Issuer, clientID: record.ClientID, subject: record.Subject, account: record.AccountID}
}

func sameCodexIdentity(a, b codexIdentity) bool {
	return a.issuer == b.issuer && a.clientID == b.clientID && a.subject == b.subject && a.account == b.account
}

func recordVersion(record codexImagesRecord) string {
	return tokenVersion(record.AccessToken, record.RefreshToken)
}

func tokenVersion(access, refresh string) string {
	digest := sha256.Sum256([]byte(access + "\x00" + refresh))
	return hex.EncodeToString(digest[:])
}

func nonEmptySecrets(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	secrets := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		secrets = append(secrets, value)
	}
	return secrets
}

func tokenExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
	}
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Expires json.Number `json:"exp"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if decoder.Decode(&claims) != nil {
		return time.Time{}, false
	}
	seconds, err := claims.Expires.Int64()
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0), true
}

func externalUserIdentity(idToken, accessToken string) string {
	for _, token := range []string{idToken, accessToken} {
		var identity codexExternalJWTIdentity
		if !decodeJWTClaims(token, &identity) {
			continue
		}
		if identity.Auth.ChatGPTUserID != "" {
			return identity.Auth.ChatGPTUserID
		}
		if identity.Auth.UserID != "" {
			return identity.Auth.UserID
		}
		if identity.Subject != "" {
			return identity.Subject
		}
	}
	return ""
}

func decodeJWTClaims(token string, target any) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
	}
	return err == nil && json.Unmarshal(payload, target) == nil
}

func responseExpiry(tokens codexTokenResponse) (time.Time, bool) {
	if tokens.ExpiresIn > 0 {
		return time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second), true
	}
	return tokenExpiry(tokens.AccessToken)
}

func rawTokenString(raw json.RawMessage) string {
	var token string
	if json.Unmarshal(raw, &token) == nil {
		return token
	}
	var claims struct {
		Raw string `json:"raw_jwt"`
	}
	if json.Unmarshal(raw, &claims) == nil {
		return claims.Raw
	}
	return ""
}

func codexIssuer(opts Options) string {
	if strings.TrimSpace(opts.CodexIssuer) != "" {
		return strings.TrimRight(strings.TrimSpace(opts.CodexIssuer), "/")
	}
	return CodexIssuer
}

func validCodexIssuer(issuer string) bool {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	return host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}

func codexClientID(opts Options) string {
	if strings.TrimSpace(opts.CodexClientID) != "" {
		return strings.TrimSpace(opts.CodexClientID)
	}
	return CodexClientID
}

func codexCallbackPorts(opts Options) []int {
	if opts.CodexCallbackPort > 0 {
		return []int{opts.CodexCallbackPort}
	}
	// Unlike dynamic ChatGPT plan-sharing registration, this fixed client has
	// registered redirects. Match the pinned Codex login source's two ports.
	return []int{CodexPort, CodexFallbackPort}
}

func sameOrigin(issuer, endpoint string) bool {
	base, err := url.Parse(issuer)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return false
	}
	target, err := url.Parse(endpoint)
	if err != nil || target.Scheme != base.Scheme || !strings.EqualFold(target.Host, base.Host) || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return false
	}
	return true
}
