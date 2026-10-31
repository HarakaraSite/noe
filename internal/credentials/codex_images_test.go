package credentials

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type codexOAuthFixture struct {
	server *httptest.Server
	key    *rsa.PrivateKey

	mu             sync.Mutex
	refreshCalls   int
	lastRefresh    map[string]string
	omitRefresh    bool
	refreshAccess  string
	onCodeExchange func(http.ResponseWriter, *http.Request)
}

type blockingCodexTokenHTTP struct {
	issuer   string
	started  chan struct{}
	canceled chan struct{}
	once     sync.Once
	stopOnce sync.Once
}

func (c *blockingCodexTokenHTTP) Do(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodGet {
		payload := fmt.Sprintf(`{"issuer":%q,"token_endpoint":%q,"jwks_uri":%q}`,
			c.issuer, c.issuer+"/oauth/token", c.issuer+"/jwks")
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload)), Request: request}, nil
	}
	c.once.Do(func() { close(c.started) })
	<-request.Context().Done()
	c.stopOnce.Do(func() { close(c.canceled) })
	return nil, request.Context().Err()
}

func newCodexOAuthFixture(t *testing.T) *codexOAuthFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &codexOAuthFixture{key: key}
	f.server = httptest.NewServer(http.HandlerFunc(f.serveHTTP))
	t.Cleanup(f.server.Close)
	return f
}

func (f *codexOAuthFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration", "/discovery":
		_, _ = fmt.Fprintf(w, `{"issuer":%q,"token_endpoint":%q,"jwks_uri":%q}`,
			f.server.URL, f.server.URL+"/oauth/token", f.server.URL+"/jwks")
	case "/jwks":
		_, _ = fmt.Fprintf(w, `{"keys":[{"kid":"fixture","kty":"RSA","n":%q,"e":"AQAB"}]}`,
			base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()))
	case "/oauth/token":
		if r.Header.Get("User-Agent") != "noe" || r.Header.Get("originator") != "noe" {
			http.Error(w, "missing NOE originator", http.StatusBadRequest)
			return
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			var request map[string]string
			if json.NewDecoder(r.Body).Decode(&request) != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.refreshCalls++
			f.lastRefresh = request
			omitRefresh, refreshAccess := f.omitRefresh, f.refreshAccess
			f.mu.Unlock()
			if _, exists := request["resource"]; exists {
				http.Error(w, "unexpected resource", http.StatusBadRequest)
				return
			}
			if _, exists := request["scope"]; exists {
				http.Error(w, "unexpected scope", http.StatusBadRequest)
				return
			}
			if refreshAccess == "" {
				refreshAccess = "fixture-access-rotated"
			}
			response := map[string]any{"access_token": refreshAccess, "expires_in": 3600}
			if !omitRefresh {
				response["refresh_token"] = "fixture-refresh-rotated"
			}
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		if f.onCodeExchange == nil {
			http.Error(w, "unexpected authorization-code exchange", http.StatusBadRequest)
			return
		}
		f.onCodeExchange(w, r)
	default:
		http.NotFound(w, r)
	}
}

func TestCodexImagesRefreshPreservesRefreshTokenWhenOmitted(t *testing.T) {
	fixture := newCodexOAuthFixture(t)
	fixture.omitRefresh = true
	path := filepath.Join(t.TempDir(), "managed.json")
	record := fixtureManagedRecord("account-1", 1, "fixture-access-old", "fixture-refresh-old", time.Now().Add(-time.Minute))
	record.Issuer = fixture.server.URL
	writeManagedFixture(t, path, record)
	if _, err := NewCodexImages(path, CodexImagesManaged, fixture.options()).Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	saved, err := readCodexImagesRecord(path, codexIdentity{issuer: fixture.server.URL, clientID: "fixture-client"})
	if err != nil || saved.RefreshToken != "fixture-refresh-old" {
		t.Fatalf("omitted refresh token was not preserved: refresh=%q err=%v", saved.RefreshToken, err)
	}
}

func (f *codexOAuthFixture) options() Options {
	return Options{HTTPClient: f.server.Client(), DiscoveryURL: f.server.URL + "/.well-known/openid-configuration",
		CodexIssuer: f.server.URL, CodexClientID: "fixture-client"}
}

func (f *codexOAuthFixture) refreshCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshCalls
}

func (f *codexOAuthFixture) refreshRequest() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := make(map[string]string, len(f.lastRefresh))
	for key, value := range f.lastRefresh {
		copy[key] = value
	}
	return copy
}

func fixtureManagedRecord(accountID string, generation uint64, access, refresh string, expires time.Time) codexImagesRecord {
	return codexImagesRecord{
		SchemaVersion: codexImagesRecordSchema, Kind: codexImagesCredentialKind,
		Issuer: "http://issuer.test", ClientID: "fixture-client", Subject: "fixture-subject",
		AccountID: accountID, Email: "fixture@example.test", AccessToken: access,
		RefreshToken: refresh, IDToken: "fixture-id-token", ExpiresAt: expires,
		SavedAt: time.Now().UTC(), Generation: generation,
	}
}

func writeManagedFixture(t *testing.T, path string, record codexImagesRecord) {
	t.Helper()
	if err := writeCodexImagesRecord(path, record); err != nil {
		t.Fatal(err)
	}
}

func TestCodexImagesSnapshotRefreshesAndPersistsRotation(t *testing.T) {
	fixture := newCodexOAuthFixture(t)
	path := filepath.Join(t.TempDir(), "managed.json")
	record := fixtureManagedRecord("account-1", 4, "fixture-access-old", "fixture-refresh-old", time.Now().Add(-time.Minute))
	record.Issuer = fixture.server.URL
	writeManagedFixture(t, path, record)
	options := fixture.options()
	snapshot, err := NewCodexImages(path, CodexImagesManaged, options).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Access != "fixture-access-rotated" || snapshot.AccountID != "account-1" {
		t.Fatalf("unexpected refreshed snapshot: access=%q account=%q", snapshot.Access, snapshot.AccountID)
	}
	if fixture.refreshCount() != 1 {
		t.Fatalf("refresh calls = %d, want 1", fixture.refreshCount())
	}
	request := fixture.refreshRequest()
	if request["grant_type"] != "refresh_token" || request["client_id"] != "fixture-client" || request["refresh_token"] != "fixture-refresh-old" || request["refresh_reason"] != "proactive_expiry" {
		t.Fatalf("incorrect refresh grant: %#v", request)
	}
	if _, ok := request["resource"]; ok {
		t.Fatal("Codex refresh unexpectedly included a resource")
	}
	saved, err := readCodexImagesRecord(path, codexIdentity{issuer: fixture.server.URL, clientID: "fixture-client"})
	if err != nil || saved.RefreshToken != "fixture-refresh-rotated" || saved.Generation != 5 || saved.IDToken != "fixture-id-token" || !saved.ExpiresAt.After(time.Now()) {
		t.Fatalf("rotated credentials were not persisted: generation=%d refresh=%q err=%v", saved.Generation, saved.RefreshToken, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("managed credential mode=%v err=%v", info.Mode(), err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(encoded), "fixture-access") || strings.Contains(string(encoded), "fixture-refresh") || strings.Contains(string(encoded), "fixture-id-token") {
		t.Fatalf("snapshot JSON exposed credentials: %s err=%v", encoded, err)
	}
	if len(snapshot.Secrets) != 3 {
		t.Fatalf("redaction secrets = %d, want access, refresh and ID tokens", len(snapshot.Secrets))
	}
}

func TestCodexImagesConcurrentUnauthorizedRecoveryRotatesOnce(t *testing.T) {
	fixture := newCodexOAuthFixture(t)
	path := filepath.Join(t.TempDir(), "managed.json")
	record := fixtureManagedRecord("account-1", 1, "fixture-access-old", "fixture-refresh-old", time.Now().Add(time.Hour))
	record.Issuer = fixture.server.URL
	writeManagedFixture(t, path, record)
	source := NewCodexImages(path, CodexImagesManaged, fixture.options())
	rejected, err := source.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	const callers = 8
	start := make(chan struct{})
	results := make(chan CodexSnapshot, callers)
	errorsOut := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			got, err := source.RecoverUnauthorized(context.Background(), rejected)
			if err != nil {
				errorsOut <- err
				return
			}
			results <- got
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsOut)
	for err := range errorsOut {
		t.Errorf("recovery failed: %v", err)
	}
	for got := range results {
		if got.Access != "fixture-access-rotated" || got.AccountID != rejected.AccountID {
			t.Errorf("unexpected recovery snapshot: access=%q account=%q", got.Access, got.AccountID)
		}
	}
	if fixture.refreshCount() != 1 {
		t.Fatalf("concurrent 401 recovery made %d refreshes, want 1", fixture.refreshCount())
	}
	if request := fixture.refreshRequest(); request["refresh_reason"] != "unauthorized_recovery" {
		t.Fatalf("401 did not request forced recovery: %#v", request)
	}
}

func TestCodexImagesUnauthorizedRecoveryReusesNewerGenerationAndRejectsChangedAccount(t *testing.T) {
	fixture := newCodexOAuthFixture(t)
	path := filepath.Join(t.TempDir(), "managed.json")
	old := fixtureManagedRecord("account-1", 2, "fixture-access-old", "fixture-refresh-old", time.Now().Add(time.Hour))
	old.Issuer = fixture.server.URL
	writeManagedFixture(t, path, old)
	source := NewCodexImages(path, CodexImagesManaged, fixture.options())
	rejected, err := source.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	newer := old
	newer.Generation++
	newer.AccessToken = "fixture-access-newer"
	newer.RefreshToken = "fixture-refresh-newer"
	writeManagedFixture(t, path, newer)
	got, err := source.RecoverUnauthorized(context.Background(), rejected)
	if err != nil || got.Access != "fixture-access-newer" || fixture.refreshCount() != 0 {
		t.Fatalf("newer generation was not reused: access=%q refreshes=%d err=%v", got.Access, fixture.refreshCount(), err)
	}
	changed := newer
	changed.AccountID = "another-account"
	changed.Generation++
	writeManagedFixture(t, path, changed)
	if _, err := source.RecoverUnauthorized(context.Background(), rejected); err == nil || !strings.Contains(err.Error(), "changed identity") {
		t.Fatalf("changed account was not rejected: %v", err)
	}
	if fixture.refreshCount() != 0 {
		t.Fatalf("identity change triggered a refresh: %d", fixture.refreshCount())
	}
}

func TestCodexImagesExternalReadonlyReloadsWithoutLockOrWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	oldAccess := externalFixtureAccessToken(time.Now().Add(time.Hour))
	writeExternalFixture(t, path, oldAccess, "account-1", "fixture-refresh", "fixture-id")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := NewCodexImages(path, CodexImagesExternalRO, Options{})
	old, err := source.Snapshot(context.Background())
	if err != nil || old.Access != oldAccess || old.AccountID != "account-1" {
		t.Fatalf("external snapshot: access matched=%v account=%q err=%v", old.Access == oldAccess, old.AccountID, err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("external readonly created lock file: %v", err)
	}
	afterSnapshot, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, afterSnapshot) {
		t.Fatalf("external snapshot modified its source: %v", err)
	}
	newAccess := externalFixtureAccessToken(time.Now().Add(2 * time.Hour))
	writeExternalFixture(t, path, newAccess, "account-1", "fixture-refresh-rotated", "fixture-id-rotated")
	beforeRecovery, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.RecoverUnauthorized(context.Background(), old)
	if err != nil || got.Access != newAccess || got.AccountID != old.AccountID {
		t.Fatalf("external updated token not reused: access matched=%v err=%v", got.Access == newAccess, err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(beforeRecovery, unchanged) {
		t.Fatalf("external 401 recovery modified its source file: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("external readonly created lock file during recovery: %v", err)
	}
	apiKeyFile := filepath.Join(t.TempDir(), "api-key.json")
	if err := os.WriteFile(apiKeyFile, []byte(`{"auth_mode":"api_key","OPENAI_API_KEY":"fixture-key","tokens":{"access_token":"unused","account_id":"account-1"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCodexImages(apiKeyFile, CodexImagesExternalRO, Options{}).Snapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "does not contain Codex ChatGPT") {
		t.Fatalf("API-key record was not rejected as a mismatch: %v", err)
	}
	if _, err := os.Stat(apiKeyFile + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("API-key mismatch created lock file: %v", err)
	}
}

func TestCodexImagesExternalReadonlyRejectsDifferentUserInSameAccount(t *testing.T) {
	t.Run("ID token user ID", func(t *testing.T) {
		assertExternalUserChangeRejected(t,
			externalFixtureAccessToken(time.Now().Add(time.Hour)), externalFixtureIdentityToken(t, "user-a"),
			externalFixtureAccessToken(time.Now().Add(2*time.Hour)), externalFixtureIdentityToken(t, "user-b"))
	})
	t.Run("access token user ID", func(t *testing.T) {
		assertExternalUserChangeRejected(t,
			externalFixtureAccessTokenWithUser(t, time.Now().Add(time.Hour), "user-a"), "legacy-no-id-token",
			externalFixtureAccessTokenWithUser(t, time.Now().Add(2*time.Hour), "user-b"), "legacy-no-id-token")
	})
}

func assertExternalUserChangeRejected(t *testing.T, oldAccess, oldID, newAccess, newID string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	writeExternalFixture(t, path, oldAccess, "shared-account", "", oldID)
	source := NewCodexImages(path, CodexImagesExternalRO, Options{})
	rejected, err := source.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writeExternalFixture(t, path, newAccess, "shared-account", "", newID)
	if _, err := source.RecoverUnauthorized(context.Background(), rejected); err == nil || !strings.Contains(err.Error(), "account changed") {
		t.Fatalf("same-account replacement with a different user was accepted: %v", err)
	}
}

func TestCodexImageLoginCallbackPKCEValidationAndSave(t *testing.T) {
	fixture := newCodexOAuthFixture(t)
	file := filepath.Join(t.TempDir(), "dedicated.json")
	options := fixture.options()
	options.CodexCallbackPort = freeCallbackPort(t)
	var challengeWant string
	attempt, err := BeginCodexImageLogin(context.Background(), file, options)
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	parsed, err := url.Parse(attempt.Info().AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	challengeWant = parsed.Query().Get("code_challenge")
	if parsed.Query().Get("originator") != "noe" || parsed.Query().Get("client_id") != "fixture-client" || parsed.Query().Get("scope") != codexOAuthScope || attempt.Info().RedirectURI != parsed.Query().Get("redirect_uri") {
		t.Fatalf("authorization request did not match the Codex profile: %s", parsed.Redacted())
	}
	state := parsed.Query().Get("state")
	nonce := parsed.Query().Get("nonce")
	fixture.onCodeExchange = func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		verifier := r.Form.Get("code_verifier")
		digest := sha256.Sum256([]byte(verifier))
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("client_id") != "fixture-client" || r.Form.Get("code") != "fixture-code" || r.Form.Get("redirect_uri") != attempt.Info().RedirectURI || base64.RawURLEncoding.EncodeToString(digest[:]) != challengeWant {
			t.Errorf("incorrect authorization-code exchange: %#v", r.Form)
		}
		idToken := signedFixtureJWT(t, fixture.key, map[string]any{
			"iss": fixture.server.URL, "sub": "fixture-subject", "email": "image@example.test",
			"aud": "fixture-client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": nonce,
			"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "account-1"},
		})
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-login-access", "refresh_token": "fixture-login-refresh", "id_token": idToken, "expires_in": 3600})
	}
	complete := make(chan struct {
		account Account
		err     error
	}, 1)
	go func() {
		account, err := attempt.Complete(context.Background())
		complete <- struct {
			account Account
			err     error
		}{account, err}
	}()
	badStateURL := attempt.Info().RedirectURI + "?code=ignored&state=wrong"
	badResponse, err := http.Get(badStateURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = badResponse.Body.Close()
	if badResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong state status = %d", badResponse.StatusCode)
	}
	callbackURL := attempt.Info().RedirectURI + "?code=fixture-code&state=" + url.QueryEscape(state)
	response, err := http.Get(callbackURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d", response.StatusCode)
	}
	result := <-complete
	if result.err != nil || result.account.Email != "image@example.test" || result.account.File != file {
		t.Fatalf("login completion: account=%+v err=%v", result.account, result.err)
	}
	public, _ := json.Marshal(result.account)
	if strings.Contains(string(public), "fixture-login-access") || strings.Contains(string(public), "fixture-login-refresh") || strings.Contains(string(public), "fixture-id-token") {
		t.Fatalf("public account metadata exposed a credential: %s", public)
	}
	record, err := readCodexImagesRecord(file, codexIdentity{issuer: fixture.server.URL, clientID: "fixture-client"})
	if err != nil || record.AccountID != "account-1" || record.AccessToken != "fixture-login-access" || record.RefreshToken != "fixture-login-refresh" || record.Generation != 1 {
		t.Fatalf("managed login was not saved: account=%q generation=%d err=%v", record.AccountID, record.Generation, err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("saved credential permissions: %v err=%v", info, err)
	}
}

func TestCodexImageLoginRejectsInvalidNonceAndCloseCancelsAttempt(t *testing.T) {
	fixture := newCodexOAuthFixture(t)
	file := filepath.Join(t.TempDir(), "dedicated.json")
	options := fixture.options()
	options.CodexCallbackPort = freeCallbackPort(t)
	attempt, err := BeginCodexImageLogin(context.Background(), file, options)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(attempt.Info().AuthorizationURL)
	query := parsed.Query()
	fixture.onCodeExchange = func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idToken := signedFixtureJWT(t, fixture.key, map[string]any{
			"iss": fixture.server.URL, "sub": "fixture-subject", "aud": "fixture-client",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": "wrong-nonce",
			"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "account-1"},
		})
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "id_token": idToken, "expires_in": 3600})
	}
	completed := make(chan error, 1)
	go func() {
		_, err := attempt.Complete(context.Background())
		completed <- err
	}()
	callback := attempt.Info().RedirectURI + "?code=fixture-code&state=" + url.QueryEscape(query.Get("state"))
	response, err := http.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if err := <-completed; err == nil || !strings.Contains(err.Error(), "identity validation failed") {
		t.Fatalf("invalid nonce was accepted: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("invalid login wrote credentials: %v", err)
	}

	cancelledFile := filepath.Join(t.TempDir(), "cancelled.json")
	options.CodexCallbackPort = freeCallbackPort(t)
	cancelled, err := BeginCodexImageLogin(context.Background(), cancelledFile, options)
	if err != nil {
		t.Fatal(err)
	}
	cancelled.Close()
	if _, err := cancelled.Complete(context.Background()); err == nil {
		t.Fatal("closed login attempt did not cancel completion")
	}
	select {
	case <-cancelled.serveDone:
	case <-time.After(time.Second):
		t.Fatal("callback listener did not stop on Close")
	}
}

func TestCodexImageLoginCloseCancelsAndJoinsBlockedExchange(t *testing.T) {
	issuer := "http://127.0.0.1:35413"
	client := &blockingCodexTokenHTTP{issuer: issuer, started: make(chan struct{}), canceled: make(chan struct{})}
	options := Options{HTTPClient: client, DiscoveryURL: issuer + "/.well-known/openid-configuration",
		CodexIssuer: issuer, CodexClientID: "fixture-client", CodexCallbackPort: freeCallbackPort(t)}
	file := filepath.Join(t.TempDir(), "managed.json")
	attemptCtx, cancelAttemptCtx := context.WithCancel(context.Background())
	defer cancelAttemptCtx()
	attempt, err := BeginCodexImageLogin(attemptCtx, file, options)
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	authorizeURL, err := url.Parse(attempt.Info().AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	completeCtx := context.Background()
	completeResult := make(chan error, 1)
	go func() {
		_, err := attempt.Complete(completeCtx)
		completeResult <- err
	}()
	callbackURL := attempt.Info().RedirectURI + "?code=fixture-code&state=" + url.QueryEscape(authorizeURL.Query().Get("state"))
	callbackResponse, err := http.Get(callbackURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = callbackResponse.Body.Close()
	if callbackResponse.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d", callbackResponse.StatusCode)
	}
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("authorization-code exchange did not start")
	}
	if completeCtx.Err() != nil {
		t.Fatalf("independent Complete context was canceled: %v", completeCtx.Err())
	}
	closed := make(chan struct{})
	go func() {
		attempt.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not cancel and join the blocked exchange")
	}
	select {
	case <-client.canceled:
	case <-time.After(time.Second):
		t.Fatal("token exchange did not observe attempt cancellation")
	}
	select {
	case err := <-completeResult:
		if err == nil {
			t.Fatal("blocked exchange unexpectedly completed successfully")
		}
	case <-time.After(time.Second):
		t.Fatal("Complete remained active after Close returned")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("credential record was written after canceled exchange: %v", err)
	}
}

func TestCodexImageLoginBeginContextCancelsBlockedExchange(t *testing.T) {
	issuer := "http://127.0.0.1:35414"
	client := &blockingCodexTokenHTTP{issuer: issuer, started: make(chan struct{}), canceled: make(chan struct{})}
	options := Options{HTTPClient: client, DiscoveryURL: issuer + "/.well-known/openid-configuration",
		CodexIssuer: issuer, CodexClientID: "fixture-client", CodexCallbackPort: freeCallbackPort(t)}
	file := filepath.Join(t.TempDir(), "managed.json")
	attemptCtx, cancelAttemptCtx := context.WithCancel(context.Background())
	attempt, err := BeginCodexImageLogin(attemptCtx, file, options)
	if err != nil {
		cancelAttemptCtx()
		t.Fatal(err)
	}
	defer attempt.Close()
	authorizeURL, err := url.Parse(attempt.Info().AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	completeResult := make(chan error, 1)
	go func() {
		_, err := attempt.Complete(context.Background())
		completeResult <- err
	}()
	callbackURL := attempt.Info().RedirectURI + "?code=fixture-code&state=" + url.QueryEscape(authorizeURL.Query().Get("state"))
	callbackResponse, err := http.Get(callbackURL)
	if err != nil {
		cancelAttemptCtx()
		t.Fatal(err)
	}
	_ = callbackResponse.Body.Close()
	if callbackResponse.StatusCode != http.StatusOK {
		cancelAttemptCtx()
		t.Fatalf("callback status = %d", callbackResponse.StatusCode)
	}
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		cancelAttemptCtx()
		t.Fatal("authorization-code exchange did not start")
	}
	cancelAttemptCtx()
	select {
	case <-client.canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("Begin context cancellation did not reach the token exchange")
	}
	select {
	case err := <-completeResult:
		if err == nil {
			t.Fatal("blocked exchange unexpectedly completed successfully")
		}
	case <-time.After(time.Second):
		t.Fatal("Complete remained active after Begin context cancellation")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("canceled Begin context wrote credentials: %v", err)
	}
}

func TestCodexImagesCustomIssuerDerivesDiscoveryURL(t *testing.T) {
	fixture := newCodexOAuthFixture(t)
	options := fixture.options()
	options.DiscoveryURL = ""
	path := filepath.Join(t.TempDir(), "managed.json")
	record := fixtureManagedRecord("account-1", 1, "access-old", "refresh-old", time.Now().Add(-time.Minute))
	record.Issuer = fixture.server.URL
	writeManagedFixture(t, path, record)
	if _, err := NewCodexImages(path, CodexImagesManaged, options).Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func writeExternalFixture(t *testing.T, path, access, accountID, refresh, idToken string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"access_token": access, "refresh_token": refresh, "id_token": idToken, "account_id": accountID,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func externalFixtureAccessToken(expires time.Time) string {
	payload, _ := json.Marshal(map[string]any{"exp": expires.Unix()})
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func externalFixtureIdentityToken(t *testing.T, userID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"sub":                         "subject-" + userID,
		"https://api.openai.com/auth": map[string]string{"chatgpt_user_id": userID},
	})
	if err != nil {
		t.Fatal(err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func externalFixtureAccessTokenWithUser(t *testing.T, expires time.Time, userID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"exp": expires.Unix(), "sub": "subject-" + userID,
		"https://api.openai.com/auth": map[string]string{"user_id": userID},
	})
	if err != nil {
		t.Fatal(err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func signedFixtureJWT(t *testing.T, key *rsa.PrivateKey, claims any) string {
	t.Helper()
	encode := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(data)
	}
	unsigned := encode(map[string]string{"alg": "RS256", "kid": "fixture"}) + "." + encode(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func freeCallbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
