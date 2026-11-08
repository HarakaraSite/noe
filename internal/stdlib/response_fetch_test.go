package stdlib_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestFetchResponseMetadataAndNullBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bytes" {
			if got := r.Header.Get("X-Byte"); got != "\xff\xc3\xa9" {
				t.Errorf("outbound ByteString=%q", got)
			}
			w.Header().Set("X-Byte", "\xff\xc3\xa9")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.Header().Add("Set-Cookie", "a=1; Path=/")
		w.Header().Add("Set-Cookie", "b=2; Path=/")
		switch r.URL.Path {
		case "/204":
			w.WriteHeader(204)
		case "/205":
			w.WriteHeader(205)
			io.WriteString(w, "discarded-body")
		case "/304":
			w.WriteHeader(304)
		default:
			io.WriteString(w, "payload")
		}
	}))
	defer server.Close()
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: &http.Client{Timeout: 4 * time.Second}}}, fmt.Sprintf(`(async()=>{
 const base=%q;
 for(const [path,method,status] of [["/204","GET",204],["/205","GET",205],["/304","GET",304],["/head","HEAD",200]]) {
  const response=await fetch(base+path,{method});
  if(response.status!==status || response.body!==null || response.bodyUsed || await response.text()!=="" || response.bodyUsed)throw Error("native null body "+path);
  const clone=response.clone();if(clone.body!==null || clone.status!==status)throw Error("clone null body");
 }
 const response=await fetch(base+"/redirect#fragment"),clone=response.clone();
 if(!response.redirected || response.url!==base+"/final" || response.type!=="basic" || clone.url!==response.url || !clone.redirected || clone.type!=="basic")throw Error("native metadata");
 if(response.headers.getSetCookie().join("|")!=="a=1; Path=/|b=2; Path=/" || [...response.headers].filter(([key])=>key==="set-cookie").length!==2)throw Error("native cookies");
 for(const value of [response,clone]) {
  if(Reflect.set(value,"status",201)||Reflect.set(value,"url","wrong"))throw Error("readonly native metadata");
  let rejected=false;try{value.headers.set("x","y");}catch(error){rejected=error instanceof TypeError;}if(!rejected)throw Error("native immutable headers");
  if(await value.text()!=="payload")throw Error("native cloned body");
 }
 const fragment=await fetch(base+"/final#fragment");if(fragment.url.includes("#"))throw Error("response URL includes fragment");await fragment.text();
 const byteRequest=new Request(base+"/bytes",{headers:{"x-byte":"\xff\xc3\xa9"}});
 Object.defineProperty(byteRequest,"headers",{get(){throw Error("shadow Request headers");}});
 if(byteRequest.clone().headers.get("x-byte")!=="\xff\xc3\xa9"||new Request(byteRequest).headers.get("x-byte")!=="\xff\xc3\xa9")throw Error("private Request header copy");
 const bytes=await fetch(byteRequest);if(bytes.headers.get("x-byte")!=="\xff\xc3\xa9")throw Error("native ByteString headers");await bytes.text();
 console.log("native response PASS");
})()`, server.URL))
	if out != "native response PASS" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}

func TestFetchResponseWithoutLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Byte", "\xff\xc3\xa9")
		w.WriteHeader(205)
		io.WriteString(w, "discarded-body")
	}))
	defer server.Close()
	client := server.Client()
	rt := quickjs.New()
	defer rt.Close()
	if err := stdlib.Install(rt, stdlib.Config{Fetch: &stdlib.Fetch{Client: client, MaxBodyBytes: 1}}); err != nil {
		t.Fatal(err)
	}
	value, err := rt.Eval(fmt.Sprintf(`(async()=>{
 const response=await fetch(%q+"/#fragment");
 if(response.body!==null || response.status!==205 || response.url!==%q+"/" || response.headers.get("x-byte")!=="\xff\xc3\xa9")throw Error("non-Loop response");
 if(await response.text()!=="" || response.bodyUsed || response.clone().body!==null)throw Error("non-Loop null body");
 let rejected=false;try{response.headers.delete("absent");}catch(error){rejected=error instanceof TypeError;}if(!rejected)throw Error("non-Loop immutable headers");
 return "PASS";
})()`, server.URL, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	value, err = value.Await(ctx)
	if err != nil || value.String() != "PASS" {
		t.Fatalf("value=%v err=%v", value, err)
	}
}
