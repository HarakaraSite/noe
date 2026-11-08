package stdlib_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestFetchResponseCloneBufferTransfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "native payload")
	}))
	defer server.Close()
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: &http.Client{Timeout: 4 * time.Second}}}, fmt.Sprintf(`(async()=>{
 const original=await fetch(%q),copy=original.clone();
 const reader=original.body.getReader(),chunk=await reader.read();
 structuredClone(chunk.value.buffer,{transfer:[chunk.value.buffer]});reader.releaseLock();
 if(await copy.text()!=="native payload"||copy.url!==original.url||copy.status!==200)throw Error("native clone changed");
 console.log("native clone transfer isolated");
})()`, server.URL))
	if out != "native clone transfer isolated" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}
