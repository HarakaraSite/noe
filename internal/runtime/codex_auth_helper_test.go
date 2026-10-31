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
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
)

// Run the unchanged helper with the real native auth binding in a separate
// process. Only the host's issuer/client/port point to a local OAuth fixture.
func runCodexImageHelperProcess() {
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	name := filepath.Join(root, "scripts", "auth-codex-images.js")
	helper, err := os.ReadFile(name)
	if err != nil {
		os.Exit(2)
	}
	var args []string
	if json.Unmarshal([]byte(os.Getenv("NOE_CODEX_IMAGES_HELPER_ARGS")), &args) != nil {
		os.Exit(2)
	}
	port, _ := strconv.Atoi(os.Getenv("NOE_CODEX_IMAGES_HELPER_PORT"))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	code, err := Execute(ctx, Source{Name: name, Code: string(helper)}, Options{
		Cwd: root, Args: args, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		InstructionDirs: []string{},
		Auth:            &credentials.Options{CodexIssuer: os.Getenv("NOE_CODEX_IMAGES_HELPER_ISSUER"), CodexClientID: "fixture-client", CodexCallbackPort: port},
	})
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, Diagnostic(err))
		code = 1
	}
	os.Exit(code)
}

func codexHelperEnv(config string) []string {
	var env []string
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "NOE_") && !strings.HasPrefix(value, "XDG_CONFIG_HOME=") {
			env = append(env, value)
		}
	}
	return append(env, "XDG_CONFIG_HOME="+config)
}

func completeImageHelper(t *testing.T, env, args []string, input string) (string, error) {
	t.Helper()
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	argv := append([]string{"run", "--no-inst", "scripts/auth-codex-images.js", "complete"}, args...)
	cmd := exec.CommandContext(ctx, runtimeTestExecutable, argv...)
	cmd.Dir, cmd.Env, cmd.Stdin = root, env, strings.NewReader(input+"\n")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func assertImageHelperRedacted(t *testing.T, output string, private ...string) {
	t.Helper()
	for _, value := range private {
		if value != "" && strings.Contains(output, value) {
			t.Fatal("helper output exposed private callback or token data")
		}
	}
}

func TestCodexImageHelperSeparateProcessRelayAndSave(t *testing.T) {
	for _, scenario := range []string{"default", "explicit-relative", "stale-pending", "exchange-failure", "replaced-pending"} {
		t.Run(scenario, func(t *testing.T) {
			config := t.TempDir()
			env := codexHelperEnv(config)
			file := filepath.Join(config, "noe", "credentials", "openai-codex-images.json")
			var args []string
			if scenario == "explicit-relative" {
				file = filepath.Join(config, "custom", "images.json")
				root, _ := filepath.Abs(filepath.Join("..", ".."))
				relative, err := filepath.Rel(root, file)
				if err != nil {
					t.Fatal(err)
				}
				args = []string{relative}
			}
			pendingDir := file + ".pending"
			if scenario == "stale-pending" {
				if err := os.MkdirAll(pendingDir, 0700); err != nil {
					t.Fatal(err)
				}
				stale := filepath.Join(pendingDir, "stale.json")
				if err := os.WriteFile(stale, []byte(`{"redirectURI":"http://127.0.0.1:1/auth/callback"}`), 0600); err != nil {
					t.Fatal(err)
				}
				old := time.Now().Add(-time.Hour)
				if err := os.Chtimes(stale, old, old); err != nil {
					t.Fatal(err)
				}
			}
			conversationFile := filepath.Join(config, "noe", "credentials", "openai-chatgpt.json")
			if err := os.MkdirAll(filepath.Dir(conversationFile), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(conversationFile, []byte("unchanged conversation credential"), 0600); err != nil {
				t.Fatal(err)
			}
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Fatal(err)
			}
			var authorization atomic.Value
			var exchanges atomic.Int32
			const code = "private-callback-code+&value"
			const access, refresh = "private-image-access", "private-image-refresh"
			var endpoint string
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					fmt.Fprintf(w, `{"issuer":%q,"token_endpoint":%q,"jwks_uri":%q}`, endpoint, endpoint+"/token", endpoint+"/jwks")
				case "/jwks":
					fmt.Fprintf(w, `{"keys":[{"kid":"fixture","kty":"RSA","n":%q,"e":"AQAB"}]}`, base64.RawURLEncoding.EncodeToString(key.N.Bytes()))
				case "/token":
					exchanges.Add(1)
					authorize, _ := url.Parse(authorization.Load().(string))
					_ = r.ParseForm()
					digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
					if r.Form.Get("code") != code || r.Form.Get("client_id") != "fixture-client" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != authorize.Query().Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(digest[:]) != authorize.Query().Get("code_challenge") {
						t.Error("relay did not preserve native code exchange and PKCE")
					}
					if scenario == "replaced-pending" {
						if err := os.WriteFile(filepath.Join(pendingDir, "another-login.json"), []byte(`{"redirectURI":"http://127.0.0.1:1/auth/callback"}`), 0600); err != nil {
							t.Error("could not simulate a replacement pending record")
						}
					}
					if scenario == "exchange-failure" || scenario == "replaced-pending" {
						http.Error(w, "not included in diagnostics: "+code, http.StatusUnauthorized)
						return
					}
					encode := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
					unsigned := encode(map[string]string{"alg": "RS256", "kid": "fixture"}) + "." + encode(map[string]any{
						"iss": endpoint, "sub": "fixture-user", "aud": "fixture-client", "nonce": authorize.Query().Get("nonce"),
						"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
						"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "fixture-account"},
					})
					hash := sha256.Sum256([]byte(unsigned))
					signature, signErr := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
					if signErr != nil {
						t.Error("could not sign fixture identity")
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "refresh_token": refresh, "id_token": unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), "expires_in": 3600})
				default:
					t.Error("unexpected fixture endpoint")
					http.NotFound(w, r)
				}
			}))
			defer fixture.Close()
			endpoint = fixture.URL
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()
			executable, _ := os.Executable()
			encoded, _ := json.Marshal(append([]string{"login"}, args...))
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			login := exec.CommandContext(ctx, executable)
			login.Env = append(append([]string{}, env...), "NOE_CODEX_IMAGES_HELPER_TEST=1", "NOE_CODEX_IMAGES_HELPER_ARGS="+string(encoded), "NOE_CODEX_IMAGES_HELPER_ISSUER="+endpoint, "NOE_CODEX_IMAGES_HELPER_PORT="+strconv.Itoa(port))
			output := &loginOutput{url: make(chan string, 1), prefix: endpoint + "/oauth/authorize?"}
			var diagnostic bytes.Buffer
			login.Stdout, login.Stderr = output, &diagnostic
			if err := login.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- login.Wait() }()
			var authorize string
			select {
			case authorize = <-output.url:
			case <-done:
				t.Fatal("login exited before displaying its authorization URL")
			case <-ctx.Done():
				t.Fatal("login did not become ready")
			}
			authorization.Store(authorize)
			u, _ := url.Parse(authorize)
			callback, _ := url.Parse(u.Query().Get("redirect_uri"))
			callback.RawQuery = url.Values{"code": {code}, "state": {u.Query().Get("state")}}.Encode()
			entries, err := os.ReadDir(pendingDir)
			if err != nil {
				t.Fatal("login did not create its pending directory")
			}
			var own []os.DirEntry
			for _, entry := range entries {
				if entry.Name() != "stale.json" && strings.HasSuffix(entry.Name(), ".json") {
					own = append(own, entry)
				}
			}
			if len(own) != 1 {
				t.Fatal("login did not publish a unique pending record")
			}
			pendingFile := filepath.Join(pendingDir, own[0].Name())
			pendingBytes, err := os.ReadFile(pendingFile)
			if err != nil {
				t.Fatal("login did not create its pending record")
			}
			var pending map[string]string
			if json.Unmarshal(pendingBytes, &pending) != nil || len(pending) != 1 || pending["redirectURI"] != u.Query().Get("redirect_uri") {
				t.Fatal("pending must contain only the callback receiver")
			}
			info, _ := os.Stat(pendingFile)
			if info.Mode().Perm() != 0600 {
				t.Fatal("pending record is not private")
			}
			info, _ = os.Stat(pendingDir)
			if info.Mode().Perm() != 0700 {
				t.Fatal("pending directory is not private")
			}
			for _, mismatch := range []string{"path", "origin", "fragment", "userinfo"} {
				bad := *callback
				switch mismatch {
				case "path":
					bad.Path += "/wrong"
				case "origin":
					bad.Host = "127.0.0.1:1"
				case "fragment":
					bad.Fragment = "private-fragment"
				case "userinfo":
					bad.User = url.UserPassword("private-user", "private-password")
				}
				text, err := completeImageHelper(t, env, args, bad.String())
				if err == nil || !strings.Contains(text, "does not match") {
					t.Fatal("mismatched URL did not produce actionable rejection")
				}
				assertImageHelperRedacted(t, text, code, url.QueryEscape(code), bad.String(), access, refresh)
			}
			wrong := *callback
			query := wrong.Query()
			query.Set("state", "wrong-state")
			wrong.RawQuery = query.Encode()
			text, err := completeImageHelper(t, env, args, wrong.String())
			if err == nil || !strings.Contains(text, "rejected the callback") || exchanges.Load() != 0 {
				t.Fatal("native state validation was bypassed")
			}
			assertImageHelperRedacted(t, text, code, url.QueryEscape(code), wrong.String())
			text, err = completeImageHelper(t, env, args, callback.String())
			if err != nil || !strings.Contains(text, "Callback delivered") {
				t.Fatal("separate complete process did not relay the callback")
			}
			assertImageHelperRedacted(t, text, code, url.QueryEscape(code), callback.String(), access, refresh)
			loginErr := <-done
			failed := scenario == "exchange-failure" || scenario == "replaced-pending"
			if (loginErr != nil) != failed || exchanges.Load() != 1 {
				t.Fatal("native login completion did not match the fixture outcome")
			}
			output.mu.Lock()
			loginText := output.text.String()
			output.mu.Unlock()
			assertImageHelperRedacted(t, loginText+diagnostic.String(), code, url.QueryEscape(code), callback.String(), access, refresh)
			if scenario == "replaced-pending" {
				data, _ := os.ReadFile(filepath.Join(pendingDir, "another-login.json"))
				if !bytes.Contains(data, []byte("127.0.0.1:1")) {
					t.Fatal("login removed another attempt's pending record")
				}
			}
			if _, err := os.Stat(pendingFile); !os.IsNotExist(err) {
				t.Fatal("completed login left its pending record")
			}
			if !failed {
				data, err := os.ReadFile(file)
				var record map[string]any
				if err != nil || json.Unmarshal(data, &record) != nil || record["access_token"] != access || record["refresh_token"] != refresh || !strings.Contains(loginText, "credentials saved") {
					t.Fatal("native login did not save the image credentials")
				}
			} else if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Fatal("failed exchange wrote credentials")
			}
			guard, _ := os.ReadFile(conversationFile)
			if string(guard) != "unchanged conversation credential" {
				t.Fatal("image helper modified conversation authentication")
			}
		})
	}
}

func TestCodexImageHelperMissingPendingAndStoppedListener(t *testing.T) {
	config := t.TempDir()
	env := codexHelperEnv(config)
	const private = "private-code-not-for-output"
	text, err := completeImageHelper(t, env, nil, "http://127.0.0.1:1455/auth/callback?code="+private)
	if err == nil || !strings.Contains(text, "Start login in terminal A") {
		t.Fatal("missing pending record did not explain how to start login")
	}
	assertImageHelperRedacted(t, text, private)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	receiver := "http://" + listener.Addr().String() + "/auth/callback"
	_ = listener.Close()
	file := filepath.Join(config, "selected-image.json")
	pending, _ := json.Marshal(map[string]string{"redirectURI": receiver})
	if err := os.Mkdir(file+".pending", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(file+".pending", "ended-login.json"), pending, 0600); err != nil {
		t.Fatal(err)
	}
	text, err = completeImageHelper(t, env, []string{file}, "://invalid?code="+private)
	if err == nil || !strings.Contains(text, "Invalid callback URL") {
		t.Fatal("malformed callback URL did not produce a sanitized error")
	}
	assertImageHelperRedacted(t, text, private)
	text, err = completeImageHelper(t, env, []string{file}, receiver+"?code="+private)
	if err == nil || !strings.Contains(text, "no longer reachable") || !strings.Contains(text, "Run login again") {
		t.Fatal("stopped listener did not produce actionable sanitized error")
	}
	assertImageHelperRedacted(t, text, private, receiver+"?code="+private)
}
