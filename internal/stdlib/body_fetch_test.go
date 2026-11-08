package stdlib_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestFetchClonedAndTransferredFiniteBodiesReplay(t *testing.T) {
	var admissions atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if r.Method != "POST" || r.ContentLength != 2 || string(body) != "AB" {
			t.Errorf("finite request method=%s length=%d body=%q", r.Method, r.ContentLength, body)
		}
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "/echo")
			w.WriteHeader(307)
			return
		}
		w.Write(body)
	}))
	defer server.Close()
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: &http.Client{Timeout: 4 * time.Second}, Allow: func(*http.Request) error {
		admissions.Add(1)
		return nil
	}}}, fmt.Sprintf(`(async()=>{
 const input=new Uint8Array([88,65,66,99]);
 const original=new Request(%q+"/redirect",{method:"POST",body:input.subarray(1,3)});input[1]=90;
 const copy=original.clone(),transferred=new Request(copy);
 if(!copy.bodyUsed||!copy.body.locked)throw Error("transfer did not own the source");
 const bodies=await Promise.all([original,transferred].map(async request=>{
   const response=await fetch(request);if(!response.redirected)throw Error("307 was not followed");return response.text();
 }));
 if(bodies.join()!=="AB,AB")throw Error("finite body replay changed");
 console.log("finite clones replayed");
})()`, server.URL))
	if out != "finite clones replayed" || stderr != "" || admissions.Load() != 4 {
		t.Fatalf("stdout=%q stderr=%q admissions=%d", out, stderr, admissions.Load())
	}
}

func TestFetchFiniteBufferingObservesAbort(t *testing.T) {
	var admissions atomic.Int64
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Allow: func(*http.Request) error {
		admissions.Add(1)
		return fmt.Errorf("aborted request reached native admission")
	}}}, `(async()=>{
 for(const mode of ["direct","clone","transfer"]) {
  const controller=new AbortController(),reason={mode};
  const source=new Request("http://fixture/",{method:"POST",body:"payload",signal:controller.signal});
  const request=mode==="clone"?source.clone():mode==="transfer"?new Request(source):source;
  const pending=fetch(request);controller.abort(reason);
  let failure;try{await pending;}catch(error){failure=error;}
  if(failure!==reason)throw Error("buffered abort was lost: "+mode);
 }
 console.log("buffered abort observed");
})()`)
	if out != "buffered abort observed" || stderr != "" || admissions.Load() != 0 {
		t.Fatalf("stdout=%q stderr=%q admissions=%d", out, stderr, admissions.Load())
	}
}

func TestFetchFiniteBufferingSnapshotsHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		io.WriteString(w, r.Header.Get("X-Test"))
	}))
	defer server.Close()
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: &http.Client{Timeout: 4 * time.Second}}}, fmt.Sprintf(`(async()=>{
 for(const mode of ["direct","clone","transfer"]) {
  const source=new Request(%q,{method:"POST",body:"payload",headers:{"x-test":"initial"}});
  const request=mode==="clone"?source.clone():mode==="transfer"?new Request(source):source;
  const pending=fetch(request);request.headers.set("x-test","changed after fetch");
  if(await(await pending).text()!=="initial")throw Error("late headers were sent: "+mode);
 }
 console.log("entry headers preserved");
})()`, server.URL))
	if out != "entry headers preserved" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}
