package credentials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// CodexImageLoginInfo is safe to return to the helper. It contains an OAuth
// URL and a callback address, but no code verifier, token, or account secret.
type CodexImageLoginInfo struct {
	ID               string `json:"id"`
	AuthorizationURL string `json:"authorizationURL"`
	RedirectURI      string `json:"redirectURI"`
}

type codexLoginCallback struct {
	code string
	err  error
}

// CodexImageLogin owns a single loopback OAuth attempt. The callback listener,
// PKCE values, token exchange, validation, and persistence remain in Go.
type CodexImageLogin struct {
	info           CodexImageLoginInfo
	file           string
	opts           Options
	issuer         string
	clientID       string
	state          string
	nonce          string
	verifier       string
	ctx            context.Context
	cancel         context.CancelFunc
	listener       net.Listener
	server         *http.Server
	callback       chan codexLoginCallback
	serveDone      chan struct{}
	stopOnce       sync.Once
	mu             sync.Mutex
	received       bool
	completed      bool
	closed         bool
	completionDone chan struct{}
}

// BeginCodexImageLogin creates a dedicated Codex OAuth grant attempt. It binds
// a loopback callback and constructs the authorization URL without network or
// credential-file access.
func BeginCodexImageLogin(ctx context.Context, file string, opts Options) (*CodexImageLogin, error) {
	if ctx == nil || strings.TrimSpace(file) == "" {
		return nil, errors.New("Codex image login requires a context and credential file")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	issuer, clientID := codexIssuer(opts), codexClientID(opts)
	if !validCodexIssuer(issuer) {
		return nil, errors.New("configured Codex issuer is not trusted")
	}
	listener, err := listenCodexImageCallback(codexCallbackPorts(opts))
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/auth/callback", port)
	state, err := randomOAuthValue(32)
	if err != nil {
		listener.Close()
		return nil, errors.New("could not prepare the Codex image login")
	}
	nonce, err := randomOAuthValue(32)
	if err != nil {
		listener.Close()
		return nil, errors.New("could not prepare the Codex image login")
	}
	verifier, err := randomOAuthValue(32)
	if err != nil {
		listener.Close()
		return nil, errors.New("could not prepare the Codex image login")
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	infoID, err := randomOAuthValue(24)
	if err != nil {
		listener.Close()
		return nil, errors.New("could not prepare the Codex image login")
	}
	authorize, err := url.Parse(strings.TrimRight(issuer, "/") + "/oauth/authorize")
	if err != nil {
		listener.Close()
		return nil, errors.New("invalid Codex image login issuer")
	}
	query := authorize.Query()
	for key, value := range map[string]string{
		"client_id": clientID, "response_type": "code", "redirect_uri": redirectURI,
		"scope": codexOAuthScope, "state": state, "nonce": nonce,
		"code_challenge_method": "S256", "code_challenge": challenge,
		"originator": "noe", "id_token_add_organizations": "true",
		"codex_cli_simplified_flow": "true",
	} {
		query.Set(key, value)
	}
	authorize.RawQuery = query.Encode()
	loginCtx, cancel := context.WithCancel(ctx)
	a := &CodexImageLogin{
		info: CodexImageLoginInfo{ID: infoID, AuthorizationURL: authorize.String(), RedirectURI: redirectURI},
		file: file, opts: opts, issuer: issuer, clientID: clientID, state: state, nonce: nonce,
		verifier: verifier, ctx: loginCtx, cancel: cancel, listener: listener,
		callback: make(chan codexLoginCallback, 1), serveDone: make(chan struct{}),
	}
	a.server = &http.Server{Handler: http.HandlerFunc(a.handleCallback), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		defer close(a.serveDone)
		_ = a.server.Serve(a.listener)
	}()
	return a, nil
}

func listenCodexImageCallback(ports []int) (net.Listener, error) {
	for _, port := range ports {
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
		if err == nil {
			return listener, nil
		}
	}
	return nil, errors.New("could not start the Codex image login callback listener: configured callback ports are in use or unavailable; finish a previous login and try again")
}

func (a *CodexImageLogin) Info() CodexImageLoginInfo { return a.info }

func (a *CodexImageLogin) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/auth/callback" {
		http.NotFound(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(a.state)) != 1 {
		http.Error(w, "Invalid sign-in state", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	if a.received {
		a.mu.Unlock()
		http.Error(w, "Sign-in callback already received", http.StatusConflict)
		return
	}
	callback := codexLoginCallback{}
	if r.URL.Query().Get("error") != "" {
		callback.err = errors.New("Codex image sign-in was not authorized")
	} else {
		callback.code = r.URL.Query().Get("code")
		if callback.code == "" {
			callback.err = errors.New("Codex image sign-in callback was incomplete")
		}
	}
	a.received = true
	a.callback <- callback
	a.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if callback.err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Sign-in was not authorized. Return to NOE.\n"))
		return
	}
	_, _ = w.Write([]byte("Sign-in response received. Return to NOE.\n"))
}

// Complete waits for this attempt's callback, exchanges the code, validates
// the signed identity, and writes a private managed credential record.
func (a *CodexImageLogin) Complete(ctx context.Context) (Account, error) {
	if a == nil || ctx == nil {
		return Account{}, errors.New("invalid Codex image login attempt")
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return Account{}, context.Canceled
	}
	if a.completed {
		a.mu.Unlock()
		return Account{}, errors.New("Codex image login attempt was already completed")
	}
	a.completed = true
	done := make(chan struct{})
	a.completionDone = done
	a.mu.Unlock()
	completeCtx, cancel := context.WithCancel(ctx)
	stopAttemptCancel := context.AfterFunc(a.ctx, cancel)
	defer func() {
		stopAttemptCancel()
		cancel()
		a.stopAttempt()
		close(done)
	}()
	select {
	case <-completeCtx.Done():
		return Account{}, completeCtx.Err()
	case callback := <-a.callback:
		if callback.err != nil {
			return Account{}, callback.err
		}
		return a.exchangeAndSave(completeCtx, callback.code)
	}
}

func (a *CodexImageLogin) exchangeAndSave(ctx context.Context, code string) (Account, error) {
	c := NewChatGPT("", a.opts)
	d, err := discoverCodex(ctx, a.opts, c.client)
	if err != nil || d.Issuer != a.issuer {
		return Account{}, errors.New("trusted Codex sign-in service discovery failed")
	}
	form := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {a.clientID}, "code": {code},
		"redirect_uri": {a.info.RedirectURI}, "code_verifier": {a.verifier},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Account{}, errors.New("invalid trusted Codex token endpoint")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "noe")
	request.Header.Set("originator", "noe")
	response, err := c.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return Account{}, ctx.Err()
		}
		return Account{}, errors.New("Codex image authorization-code exchange failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Account{}, fmt.Errorf("Codex image authorization-code exchange failed (HTTP %d)", response.StatusCode)
	}
	var tokens codexTokenResponse
	if json.UnmarshalRead(io.LimitReader(response.Body, 1<<20), &tokens) != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.IDToken == "" {
		return Account{}, errors.New("Codex image authorization-code response is incomplete")
	}
	claims, err := c.verifyIDToken(ctx, d, tokens.IDToken, a.clientID, a.nonce)
	if err != nil || claims.Auth.AccountID == "" {
		return Account{}, errors.New("Codex image sign-in identity validation failed")
	}
	expires, ok := responseExpiry(tokens)
	if !ok || !expires.After(time.Now().Add(codexRefreshMargin)) {
		return Account{}, errors.New("Codex image sign-in returned an unusable access token")
	}
	record := codexImagesRecord{
		SchemaVersion: codexImagesRecordSchema, Kind: codexImagesCredentialKind,
		Issuer: claims.Issuer, ClientID: a.clientID, Subject: claims.Subject,
		AccountID: claims.Auth.AccountID, Email: claims.Email,
		AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken,
		IDToken: tokens.IDToken, ExpiresAt: expires, SavedAt: time.Now().UTC(), Generation: 1,
	}
	if err := saveCodexImagesRecord(ctx, a.file, record, a.opts); err != nil {
		return Account{}, err
	}
	return Account{File: a.file, Email: record.Email, ExpiresAt: record.ExpiresAt}, nil
}

func saveCodexImagesRecord(ctx context.Context, path string, record codexImagesRecord, opts Options) error {
	err := WithFileLock(ctx, path, func() error {
		if _, err := os.Stat(path); err == nil {
			old, readErr := readCodexImagesRecord(path, codexIdentity{issuer: codexIssuer(opts), clientID: codexClientID(opts)})
			if readErr != nil {
				return errors.New("credential file is already in use; choose a separate managed Codex image file")
			}
			if !sameCodexIdentity(identityFromRecord(old), identityFromRecord(record)) {
				return errors.New("credential file belongs to another account; choose a separate managed Codex image file")
			}
			record.Generation = old.Generation + 1
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.New("could not check the managed Codex image credential file")
		}
		return writeCodexImagesRecord(path, record)
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func (a *CodexImageLogin) Close() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.closed = true
	done := a.completionDone
	a.mu.Unlock()
	a.stopAttempt()
	if done != nil {
		<-done
	}
}

func (a *CodexImageLogin) stopAttempt() {
	a.cancel()
	a.stopOnce.Do(func() {
		_ = a.server.Close()
		<-a.serveDone
	})
}

func randomOAuthValue(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
