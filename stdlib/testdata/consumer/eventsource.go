package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func eventSource(ctx context.Context) error {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "id: consumer\ndata: public API\n\n")
	}))
	defer server.Close()
	_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[consumer EventSource]", Virtual: true, Module: true, Code: fmt.Sprintf(`
const source=new EventSource(%q);
await new Promise((resolve,reject)=>{
 source.onmessage=event=>{source.close();event.data==="public API"&&event.lastEventId==="consumer"?resolve():reject(Error("SSE message"))};
 source.onerror=()=>{source.close();reject(Error("SSE connection"))};
});
if(typeof serve!=="undefined")throw Error("Fetch implicitly enabled Serve");
`, server.URL)}, stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{Client: server.Client()}}})
	return err
}
