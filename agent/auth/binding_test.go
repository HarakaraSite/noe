package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent/auth"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestInitializerInstallsHostFreeAuthAlongsideCustomLoader(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	defer rt.Close()
	cleaned := false
	view, err := stdlib.Attach(rt, loop, stdlib.Profile{
		Environment: stdlib.Environment{Cwd: t.TempDir()},
		NewLoader: func(context.Context, stdlib.Origin) (quickjs.ModuleLoader, error) {
			return func(specifier, _ string) (string, string, error) {
				if specifier == "fixture:answer" {
					return "export const value=42", specifier, nil
				}
				return "", "", errors.New("unexpected custom module: " + specifier)
			}, nil
		},
		Initialize: func(ctx context.Context, view *stdlib.Bindings) (func(), error) {
			b, err := auth.Bind(ctx, view, auth.Options{})
			if err != nil {
				return nil, err
			}
			return func() { b.Close(); cleaned = true }, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	source, err := view.Prepare(stdlib.Source{Name: "memory:auth", Module: true, Virtual: true, Code: `
import {value} from "fixture:answer";
import {save_chatgpt_credentials as save} from "noe:auth";
try { await save({file:"chosen.json"}); } catch(error) {
  globalThis.authResult=value+":"+error.message;
}`})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := rt.EvalModuleContext(ctx, source.Name, source.Code)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Await(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Get("authResult")
	if err != nil || !strings.Contains(result.String(), "42:incomplete ChatGPT sign-in result") {
		t.Fatalf("auth result=%q, %v", result.String(), err)
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	if !cleaned {
		t.Fatal("initializer cleanup was not called")
	}
	if rt.Context().Err() != nil || loop.Context().Err() != nil {
		t.Fatal("borrowed runtime or loop was closed")
	}
}

func TestBindRejectsUninitializedView(t *testing.T) {
	if _, err := auth.Bind(context.Background(), &stdlib.Bindings{}, auth.Options{}); err == nil {
		t.Fatal("uninitialized execution bindings were accepted")
	}
}

func TestJSNativeCodexLoginExportsAndCloseCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	defer rt.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	cwd := t.TempDir()
	var binding *auth.Binding
	view, err := stdlib.Attach(rt, loop, stdlib.Profile{
		Environment: stdlib.Environment{Cwd: cwd},
		Initialize: func(ctx context.Context, view *stdlib.Bindings) (func(), error) {
			var bindErr error
			binding, bindErr = auth.Bind(ctx, view, auth.Options{CodexCallbackPort: port})
			if bindErr != nil {
				return nil, bindErr
			}
			return binding.Close, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	source, err := view.Prepare(stdlib.Source{Name: "memory:codex-auth", Module: true, Virtual: true, Code: `
import {begin_codex_image_login, complete_codex_image_login} from "noe:auth";
globalThis.codexLogin = await begin_codex_image_login({file:"managed.json"});
globalThis.completeCodexLogin = complete_codex_image_login;
`})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := rt.EvalModuleContext(ctx, source.Name, source.Code)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Await(ctx); err != nil {
		t.Fatal(err)
	}
	encoded, err := rt.Eval(`JSON.stringify(codexLogin)`)
	if err != nil {
		t.Fatal(err)
	}
	var info auth.CodexImageLoginInfo
	if err := json.Unmarshal([]byte(encoded.String()), &info); err != nil || info.ID == "" || info.AuthorizationURL == "" || info.RedirectURI == "" {
		t.Fatalf("begin helper returned invalid metadata: %q err=%v", encoded.String(), err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "managed.json")); !os.IsNotExist(err) {
		t.Fatalf("begin helper touched the credential file: %v", err)
	}
	complete, err := rt.Eval(`completeCodexLogin(codexLogin.id)`)
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		binding.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("binding close did not cancel the pending JS login")
	}
	_ = complete
	callback, err := url.Parse(info.RedirectURI)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", callback.Host, 100*time.Millisecond)
	if err == nil {
		_ = connection.Close()
		t.Fatal("callback listener remained open after binding close")
	}
	if _, err := os.Stat(filepath.Join(cwd, "managed.json")); !os.IsNotExist(err) {
		t.Fatalf("canceled JS login wrote credentials: %v", err)
	}
}
