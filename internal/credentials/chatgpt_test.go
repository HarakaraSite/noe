package credentials

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestChatGPTRefreshIsSerializedAndPersistsRotation(t *testing.T) {
	var refreshes atomic.Int32
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/discovery" {
			fmt.Fprintf(w, `{"issuer":"https://auth.openai.com","token_endpoint":%q,"jwks_uri":%q}`, endpoint+"/token", endpoint+"/jwks")
			return
		}
		refreshes.Add(1)
		r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("client_id") != "issued-client" || r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("resource") != Resource {
			t.Error("incorrect refresh grant")
		}
		fmt.Fprint(w, `{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()
	endpoint = server.URL
	path := filepath.Join(t.TempDir(), "record.json")
	old := Record{Issuer: "https://auth.openai.com", Subject: "user", ClientID: "issued-client", HostID: "host", IDToken: "retained-id-token", Access: "old-access", Refresh: "old-refresh", ExpiresAt: time.Now().Add(-time.Hour), Scopes: []string{PlanScope}}
	if err := Write(path, old); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			// Separate sources simulate independent clients sharing one file.
			source := NewChatGPT(path, Options{DiscoveryURL: endpoint + "/discovery"})
			got, err := source.Token(context.Background())
			if err != nil || got != "new-access" {
				t.Errorf("refresh: token matched=%v err=%v", got == "new-access", err)
			}
		})
	}
	wg.Wait()
	got, err := Read(path)
	if err != nil || refreshes.Load() != 1 || got.Refresh != "new-refresh" || got.IDToken != old.IDToken || got.ClientID != old.ClientID || !got.ExpiresAt.After(time.Now()) {
		t.Fatalf("rotation not persisted: refreshes=%d err=%v", refreshes.Load(), err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credential permissions: %v", info.Mode())
	}
}

func TestSignInVerifiesIdentityAndSavesWithoutReturningTokens(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/discovery" {
			fmt.Fprintf(w, `{"issuer":"https://auth.openai.com","token_endpoint":%q,"jwks_uri":%q}`, endpoint+"/token", endpoint+"/jwks")
		} else {
			fmt.Fprintf(w, `{"keys":[{"kid":"test","kty":"RSA","n":%q,"e":"AQAB"}]}`, base64.RawURLEncoding.EncodeToString(key.N.Bytes()))
		}
	}))
	defer server.Close()
	endpoint = server.URL
	encode := func(value any) string {
		data, _ := json.Marshal(value)
		return base64.RawURLEncoding.EncodeToString(data)
	}
	unsigned := encode(map[string]any{"alg": "RS256", "kid": "test"}) + "." + encode(map[string]any{"iss": "https://auth.openai.com", "sub": "user", "email": "user@example.test", "aud": "issued-client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": "expected-nonce"})
	digest := sha256.Sum256([]byte(unsigned))
	signature, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	token := unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	request := SaveRequest{File: filepath.Join(t.TempDir(), "record.json"), ClientID: "issued-client", HostID: "host", Nonce: "expected-nonce", Tokens: Tokens{IDToken: token, Access: "secret-access", Refresh: "secret-refresh", ExpiresIn: 3600, Scope: PlanScope}}
	opts := Options{DiscoveryURL: endpoint + "/discovery"}
	account, err := SaveChatGPT(context.Background(), request, opts)
	if err != nil || account.Email != "user@example.test" {
		t.Fatalf("sign-in: %v", err)
	}
	public, _ := json.Marshal(account)
	if strings.Contains(string(public), "secret-") || strings.Contains(string(public), token) {
		t.Fatal("tokens returned by save")
	}
	request.Nonce = "wrong-nonce"
	if _, err := SaveChatGPT(context.Background(), request, opts); err == nil {
		t.Fatal("wrong nonce accepted")
	}
	request.Nonce = "expected-nonce"
	request.Tokens.IDToken = token[:len(token)-4] + "xxxx"
	if _, err := SaveChatGPT(context.Background(), request, opts); err == nil {
		t.Fatal("invalid signature accepted")
	}
	record, err := Read(request.File)
	if err != nil || record.Access != "secret-access" {
		t.Fatal("failed reauthorization damaged the saved account")
	}
}
