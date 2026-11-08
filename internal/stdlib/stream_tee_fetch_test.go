package stdlib_test

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

type teeResponseBody struct {
	ctx    context.Context
	first  atomic.Bool
	closed atomic.Int64
}

func (b *teeResponseBody) Read(p []byte) (int, error) {
	if b.first.CompareAndSwap(false, true) {
		return copy(p, "prefix"), nil
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *teeResponseBody) Close() error {
	b.closed.Add(1)
	return nil
}

func TestFetchCloneTeeCancellationReleasesNativeReader(t *testing.T) {
	var body *teeResponseBody
	client := &http.Client{Timeout: 4 * time.Second, Transport: uploadTransport(func(r *http.Request) (*http.Response, error) {
		body = &teeResponseBody{ctx: r.Context()}
		response := uploadResponse(r, "")
		response.Body = body
		return response, nil
	})}
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: client}}, `(async()=>{
 const original=await fetch("http://fixture/"),copy=original.clone();
 for(const response of [original,copy]) {
  const reader=response.body.getReader(),chunk=await reader.read();reader.releaseLock();
  if(new TextDecoder().decode(chunk.value)!=="prefix")throw Error("native clone prefix");
 }
 let settled=false;const first=original.body.cancel("left").then(()=>settled=true);
 for(let i=0;i<20;i++)await Promise.resolve();if(settled)throw Error("early native cancellation completion");
 await Promise.all([first,copy.body.cancel("right")]);
 if(!settled)throw Error("native cancellation did not settle");
 console.log("native clone reader released");
})()`)
	if out != "native clone reader released" || stderr != "" || body == nil || body.closed.Load() != 1 {
		var closes int64
		if body != nil {
			closes = body.closed.Load()
		}
		t.Fatalf("stdout=%q stderr=%q body present=%v closes=%d", out, stderr, body != nil, closes)
	}
	if body.ctx.Err() == nil {
		t.Fatal("native request context was not cancelled")
	}
}

var _ io.ReadCloser = (*teeResponseBody)(nil)
