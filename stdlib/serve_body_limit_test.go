package stdlib_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestServeRequestBodyLimitPublicContract(t *testing.T) {
	for _, test := range []struct {
		name, body, options string
		limit               int64
		status              int
		want                string
	}{
		{"exact bound", "abcde", "", 5, 200, "abcde"},
		{"overflow default", "abcdef", "", 5, 413, "Payload Too Large"},
		{"overflow custom error response", "abcdef", `onError(error){return new Response(error.name+"|"+error.code,{status:422});},`, 5, 422, "RangeError|ERR_HTTP_BODY_TOO_LARGE"},
		{"negative unlimited", "abcdef", "", -1, 200, "abcdef"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[Serve request cap]", Virtual: true, Module: true, Code: fmt.Sprintf(`
import {serve} from "noe:http";
await using server=serve({port:0,%s},async request=>new Response(await request.text()));
const response=await fetch(server.url,{method:"POST",body:%q});
if(response.status!==%d || await response.text()!==%q)throw Error("request cap response");
`, test.options, test.body, test.status, test.want)}, stdlib.Profile{Modules: stdlib.Modules{
				Serve: &stdlib.ServeOptions{Allow: func(string) error { return nil }, MaxBodyBytes: test.limit},
				Fetch: &stdlib.FetchOptions{},
			}})
			if err != nil || result.Code != 0 {
				t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
			}
		})
	}
}

func TestServeRequestBodyOverflowAbortsStartedResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[Serve streamed request overflow]", Virtual: true, Module: true, Code: `
import {serve} from "noe:http";
await using server=serve({port:0},request=>new Response(request.body));
let rejected=false;
try{
 const response=await fetch(server.url,{method:"POST",body:"abcdef"});
 if(response.status!==200)throw Error("started response status changed");
 try{await response.text();}catch{rejected=true;}
}catch(error){if(error.message==="started response status changed")throw error;rejected=true;}
if(!rejected)throw Error("overflow transfer ended successfully with a prefix");
`}, stdlib.Profile{Modules: stdlib.Modules{
		Serve: &stdlib.ServeOptions{Allow: func(string) error { return nil }, MaxBodyBytes: 3},
		Fetch: &stdlib.FetchOptions{},
	}})
	if err != nil || result.Code != 0 {
		t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}
