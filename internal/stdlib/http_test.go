package stdlib_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	quickjs "github.com/go-quickjs/go-quickjs"
	"noe/internal/stdlib"
)

func TestHTTPModuleConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name         string
		fetch, serve bool
		keys         string
	}{
		{"both", true, true, "fetch,serve"},
		{"fetch", true, false, "fetch"},
		{"serve", false, true, "serve"},
		{"neither", false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := quickjs.New()
			defer rt.Close()
			// A host-provided global must not turn a disabled capability into an export.
			if _, err := rt.Eval(`globalThis.fetch=()=>"host fetch";globalThis.serve=()=>"host serve";`); err != nil {
				t.Fatal(err)
			}
			cfg := stdlib.Config{}
			if tc.fetch {
				cfg.Fetch = &stdlib.Fetch{}
			}
			if tc.serve {
				cfg.Serve = &stdlib.Serve{}
			}
			if err := stdlib.Install(rt, cfg); err != nil {
				t.Fatal(err)
			}
			module, err := rt.RequireModule("noe:http", "")
			if tc.keys == "" {
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
			if err != nil || keys.String() != tc.keys {
				t.Fatalf("exports=%v err=%v, want %s", keys, err, tc.keys)
			}
			for _, name := range strings.Split(tc.keys, ",") {
				value, err := rt.Eval(`httpModule.` + name + `===globalThis.` + name)
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
 const {fetch,serve}=await import("noe:http");
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
