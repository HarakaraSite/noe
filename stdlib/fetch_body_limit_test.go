package stdlib_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

// An exact-length prefix is not EOF until the peer finishes its response.
// Aborting while the new boundary probe waits must release both executions.
func TestFetchBodyLimitProbeCancelsOnAbort(t *testing.T) {
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "abcde")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[fetch boundary abort]", Virtual: true, Module: true, Code: fmt.Sprintf(`
const controller=new AbortController(), reason=new Error("stop boundary probe");
const reader=(await fetch(%q,{signal:controller.signal})).body.getReader();
let bytes=0;
while(bytes<5){const chunk=await reader.read();if(chunk.done)throw Error("early EOF");bytes+=chunk.value.length;}
const pending=reader.read();
setTimeout(()=>controller.abort(reason),10);
let rejected=false;
try{await pending;}catch{rejected=true;}
if(!rejected)throw Error("probe resolved before peer EOF");
reader.releaseLock();
`, server.URL)}, stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{Client: server.Client(), MaxBodyBytes: 5}}})
	if err != nil || result.Code != 0 {
		t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("boundary abort did not cancel the host response")
	}
}
