package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	"forge.harakara.site/littleisland/noe/v2/agent/auth"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
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
