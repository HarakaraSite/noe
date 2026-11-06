package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func urlImports(ctx context.Context) error {
	var version atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/settings":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"value":%d}`, version.Load())
		case "/payload":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte{0xef, 0xbb, 0xbf, 0, 255, 128})
		case "/text":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "\ufeffhello")
		default:
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprintf(w, "export const value=%d;", version.Load())
		}
	}))
	defer server.Close()
	profile := stdlib.Profile{
		Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{
			Imports: map[string]string{"public/value": server.URL + "/module", "public/": server.URL + "/"},
			HTTP: &stdlib.ModuleHTTPOptions{
				Client: server.Client(),
				Allow: func(request *http.Request) error {
					if request.URL.Host != server.Listener.Addr().String() {
						return fmt.Errorf("unexpected module host %q", request.URL.Host)
					}
					return nil
				},
			},
		}),
	}
	if stdlib.URLImportCapability(profile) != stdlib.URLImportsEnabled {
		return fmt.Errorf("URL provider capability was not enabled")
	}
	for _, value := range []int32{1, 2} {
		version.Store(value)
		code := fmt.Sprintf(`import * as mapped from "public/value";
import settings from "public/settings" with {type:"json"};
import text from "public/text" with {type:"text"};
import bytes from "public/payload" with {type:"bytes"};
if(settings.value!==mapped.value||text!=="\ufeffhello"||Array.from(bytes).join()!=="239,187,191,0,255,128"||!bytes.buffer.immutable)throw Error("HTTP data consumer");
const direct=await import(%q);
if(mapped!==direct||mapped.value!==%d)throw Error("URL source or identity");
if(typeof fetch!=="undefined")throw Error("URL loader implicitly enabled fetch");`, server.URL+"/module", value)
		result, err := stdlib.Execute(ctx, stdlib.Source{
			Name: "[public URL consumer]", Virtual: true, Module: true, Code: code,
		}, profile)
		if err != nil {
			return fmt.Errorf("public URL import: %w", err)
		}
		if result.Code != 0 {
			return fmt.Errorf("public URL import exited with %d", result.Code)
		}
	}
	return nil
}
