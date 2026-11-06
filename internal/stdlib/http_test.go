package stdlib_test

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestHTTPModuleConfiguration(t *testing.T) {
	for selection := range 8 {
		fetch, serve, sockets := selection&1 != 0, selection&2 != 0, selection&4 != 0
		t.Run(string(rune('0'+selection)), func(t *testing.T) {
			rt := quickjs.New()
			defer rt.Close()
			loop := stdlib.NewLoop(rt)
			defer loop.Close()
			// A host-provided standard function must not select a host module.
			if _, err := rt.Eval(`globalThis.fetch=()=>"host fetch";`); err != nil {
				t.Fatal(err)
			}
			cfg := stdlib.Config{Loop: loop}
			var expected []string
			if fetch {
				cfg.Fetch = &stdlib.Fetch{}
			}
			if serve {
				cfg.Serve = &stdlib.Serve{}
				expected = append(expected, "serve")
			}
			if sockets {
				cfg.Sockets = &stdlib.WebSockets{}
				expected = append(expected, "createWebSocket")
				if serve {
					expected = append(expected, "upgradeWebSocket")
				}
			}
			sort.Strings(expected)
			wantKeys := strings.Join(expected, ",")
			if err := stdlib.Install(rt, cfg); err != nil {
				t.Fatal(err)
			}
			for _, removed := range []string{"node:http", "http"} {
				if _, err := rt.RequireModule(removed, ""); err == nil {
					t.Fatalf("removed HTTP module %s still installed", removed)
				}
			}
			absent, err := rt.Eval(`typeof serve==="undefined" && typeof upgradeWebSocket==="undefined"`)
			if err != nil || !absent.Bool() {
				t.Fatalf("host helpers leaked into globals: %v", err)
			}
			module, err := rt.RequireModule("noe:http", "")
			if wantKeys == "" {
				if err == nil {
					t.Fatal("HTTP module installed without a configured capability")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.Set("httpModule", module); err != nil {
				t.Fatal(err)
			}
			keys, err := rt.Eval(`Object.keys(httpModule).sort().join(",")`)
			if err != nil || keys.String() != wantKeys {
				t.Fatalf("exports=%v err=%v, want %s", keys, err, wantKeys)
			}
			for _, name := range expected {
				value, err := rt.Eval(`typeof httpModule.` + name + `==="function"`)
				if err != nil || !value.Bool() {
					t.Fatalf("%s lost function identity: %v", name, err)
				}
			}
		})
	}
}

func TestHTTPModuleUsesConfiguredPermissions(t *testing.T) {
	var fetchCalls, serveCalls int
	out, errOut := run(t, stdlib.Config{
		Fetch: &stdlib.Fetch{Allow: func(*http.Request) error {
			fetchCalls++
			return errors.New("outbound denied")
		}},
		Serve: &stdlib.Serve{Allow: func(string) error {
			serveCalls++
			return errors.New("listen denied")
		}},
	}, `(async()=>{
 const {serve}=await import("noe:http");
 try{await fetch("http://127.0.0.1:1/");throw Error("fetch allowed");}
 catch(e){if(e.cause?.message!=="outbound denied")throw e;}
 try{serve({port:0},()=>new Response("unexpected"));throw Error("serve allowed");}
 catch(e){if(!e.message.includes("listen denied"))throw e;}
 console.log("permissions retained");
})()`)
	if out != "permissions retained" || errOut != "" || fetchCalls != 1 || serveCalls != 1 {
		t.Fatalf("out=%q stderr=%q fetch=%d serve=%d", out, errOut, fetchCalls, serveCalls)
	}
}
