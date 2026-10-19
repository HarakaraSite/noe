package runtime

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
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
)

type loginOutput struct {
	mu     sync.Mutex
	text   strings.Builder
	url    chan string
	prefix string
	sent   bool
}

func (o *loginOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.text.Write(data)
	if !o.sent {
		for _, line := range strings.Split(o.text.String(), "\n") {
			if strings.HasPrefix(line, o.prefix) {
				o.url <- line
				o.sent = true
				break
			}
		}
	}
	return len(data), nil
}

func TestExternalChatGPTHelperHeadlessRegistrationAndReauthorization(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var authorization atomic.Value
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/discovery":
			fmt.Fprintf(w, `{"issuer":"https://auth.openai.com","authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`, endpoint+"/authorize", endpoint+"/token", endpoint+"/jwks")
		case "/jwks":
			fmt.Fprintf(w, `{"keys":[{"kid":"test","kty":"RSA","n":%q,"e":"AQAB"}]}`, base64.RawURLEncoding.EncodeToString(key.N.Bytes()))
		case "/token":
			authorize, _ := url.Parse(authorization.Load().(string))
			r.ParseForm()
			digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("client_id") != "issued-client" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != authorize.Query().Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(digest[:]) != authorize.Query().Get("code_challenge") {
				t.Error("incorrect helper code exchange or PKCE")
			}
			encode := func(value any) string {
				data, _ := json.Marshal(value)
				return base64.RawURLEncoding.EncodeToString(data)
			}
			unsigned := encode(map[string]string{"alg": "RS256", "kid": "test"}) + "." + encode(map[string]any{"iss": "https://auth.openai.com", "sub": "account", "aud": "issued-client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": authorize.Query().Get("nonce")})
			hash := sha256.Sum256([]byte(unsigned))
			signature, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
			json.NewEncoder(w).Encode(credentials.Tokens{IDToken: unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), Access: "secret-access", Refresh: "secret-refresh", ExpiresIn: 3600, Scope: credentials.PlanScope})
		default:
			t.Errorf("unexpected auth endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	endpoint = server.URL
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	helper, err := os.ReadFile(filepath.Join("..", "..", "scripts", "auth-chatgpt.js"))
	if err != nil {
		t.Fatal(err)
	}
	// Redirect only discovery to the fixture; callback HTTP uses the real NOE
	// loopback listener and the complete command runs in a second JS runtime.
	prefix := fmt.Sprintf(`const originalFetch=globalThis.fetch;globalThis.fetch=(input,options)=>originalFetch(String(input)==="https://auth.openai.com/.well-known/openid-configuration"?%q:input,options);`+"\n", endpoint+"/discovery")
	source := Source{Name: filepath.Join(dir, "auth-chatgpt.js"), Code: prefix + string(helper)}
	file := filepath.Join(dir, "noe", "credentials", "openai-chatgpt.json")
	for attempt := range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		output := &loginOutput{url: make(chan string, 1), prefix: endpoint + "/authorize?"}
		var diagnostic bytes.Buffer
		done := make(chan error, 1)
		go func() {
			code, err := Execute(ctx, source, Options{Args: []string{"login", file}, Stdout: output, Stderr: &diagnostic, Auth: &credentials.Options{DiscoveryURL: endpoint + "/discovery"}})
			if err == nil && code != 0 {
				err = fmt.Errorf("helper exit %d", code)
			}
			done <- err
		}()
		var authorize string
		select {
		case authorize = <-output.url:
		case err := <-done:
			cancel()
			t.Fatalf("helper exited before displaying sign-in URL: %v stderr=%s", err, &diagnostic)
		case <-ctx.Done():
			cancel()
			t.Fatal("helper did not display URL")
		}
		authorization.Store(authorize)
		u, _ := url.Parse(authorize)
		if (attempt == 0 && (u.Query().Get("client_id") != "dynamic_agent_client" || u.Query().Get("agent_name_hint") != "noe")) || (attempt == 1 && (u.Query().Get("client_id") != "issued-client" || u.Query().Has("agent_name_hint"))) {
			t.Error("returning sign-in did not reuse the issued registration")
		}
		callback, _ := url.Parse(u.Query().Get("redirect_uri"))
		q := url.Values{"state": {u.Query().Get("state")}, "code": {"test-code"}, "client_id": {"issued-client"}}
		callback.RawQuery = q.Encode()
		var completeOut bytes.Buffer
		code, err := Execute(ctx, source, Options{Args: []string{"complete", file}, Stdin: strings.NewReader(callback.String()), Stdout: &completeOut})
		if err != nil || code != 0 {
			cancel()
			t.Fatalf("headless callback: code=%d err=%v", code, err)
		}
		err = <-done
		cancel()
		if err != nil {
			t.Fatalf("login completion: %v stderr=%s", err, &diagnostic)
		}
		output.mu.Lock()
		text := output.text.String()
		output.mu.Unlock()
		if strings.Contains(text, "secret-access") || strings.Contains(text, "secret-refresh") || !strings.Contains(text, "credentials saved") {
			t.Fatal("helper did not complete cleanly without printing tokens")
		}
	}
	record, err := credentials.Read(file)
	if err != nil || record.Access != "secret-access" || record.ClientID != "issued-client" {
		t.Fatalf("saved helper credentials: %v", err)
	}
}
