package stdlib_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const requestInputContract = `
const raw = " HTTPS://EXAMPLE.test:00443/a/../☃?q=a b#fragment";
const request = new Request(raw);
check(request.url === "https://example.test/%E2%98%83?q=a%20b#fragment" && request.method === "GET", "Request normalizes string URL and preserves fragment");
for (const [given,want] of [["get","GET"],["hEaD","HEAD"],["post","POST"],["put","PUT"],["delete","DELETE"],["options","OPTIONS"],["patch","patch"],["Custom_Method","Custom_Method"],[null,"null"]]) {
  check(new Request("https://example.test/",{method:given}).method === want,"HTTP method normalization: "+given);
}
for (const method of ["", "GET ", "a/b", "a\n", "\u0000", "ÿ", "\u0100", Symbol(), "connect", "TrAcE", "TRACK"]) {
  throws("TypeError",()=>new Request("https://example.test/",{method}));
}
for (const call of [()=>new Request(),()=>new Request(undefined),()=>new Request(Symbol()),()=>new Request("relative"),()=>new Request("https://user:pass@example.test/"),()=>new Request("https://user@example.test/"),()=>new Request("https://example.test/",1)]) throws("TypeError",call);
throws("TypeError",()=>new Request("https://example.test/",{method:"TRACE"},{}));
check(new Request("data:text/plain,hello").url === "data:text/plain,hello", "Request data shape is independent of HTTP transport support");
const marker = {};
try {new Request({toString(){throw marker;}});throw Error("conversion hidden");}catch(error){check(error === marker,"Request input conversion exceptions propagate");}
const order = [];
const init = {};
for (const name of ["signal","redirect","mode","method","headers","credentials","body"]) Object.defineProperty(init,name,{get(){order.push(name);return name === "method" ? {toString(){order.push("method string");return "patch";}} : undefined;}});
new Request({toString(){order.push("input");return "https://example.test/";}},init);
check(order.join() === "input,body,credentials,headers,method,method string,mode,redirect,signal", "supported init snapshot and method conversion order");
Object.defineProperty(Object.prototype,"method",{configurable:true,get(){throw Error("default dictionary inspected prototype");}});
try {check(new Request("https://example.test/").method === "GET" && new Request("https://example.test/",null).method === "GET","default empty dictionary");}
finally {delete Object.prototype.method;}
const url = new URL("https://example.test/☃");
Object.defineProperty(url,"href",{get(){throw Error("public URL href accessed");}});
check(new Request(url).url === "https://example.test/%E2%98%83", "Request uses URL stringification rather than public href");
const source = new Request("https://example.test/",{method:"patch",redirect:"manual",credentials:"include",mode:"same-origin"});
Object.defineProperties(source,{url:{get(){throw Error("shadow url accessed");}},method:{get(){throw Error("shadow method accessed");}}});
const copied = new Request(source), cloned = source.clone();
for (const value of [copied,cloned]) check(value.url === "https://example.test/" && value.method === "patch" && value.redirect === "manual" && value.credentials === "include" && value.mode === "same-origin", "copy/clone retain private URL and method plus current metadata");
check(!Reflect.set(request,"url","https://wrong.test/") && !Reflect.set(request,"method","POST"),"readonly URL/method");
check(!Object.hasOwn(request,"url") && !Object.hasOwn(request,"method"),"URL/method are private prototype getters");
throws("TypeError",()=>Object.getOwnPropertyDescriptor(Request.prototype,"url").get.call({}));
throws("TypeError",()=>Object.getOwnPropertyDescriptor(Request.prototype,"method").get.call({}));
throws("TypeError",()=>Request.prototype.clone.call({}));
check(Object.prototype.toString.call(request) === "[object Request]" && Request.length === 1,"Request interface metadata");
for (const [input,method] of [[":invalid","POST"],["https://example.test/","TRACE"],["https://example.test/",""]]) {
  let pulls=0,cancels=0;
  const body = new ReadableStream({pull(){pulls++;},cancel(){cancels++;}},{highWaterMark:0});
  throws("TypeError",()=>new Request(input,{method,body}));
  check(!body.locked && pulls === 0 && cancels === 0,"input validation does not acquire upload source");
}
const original = new Request("https://example.test/",{method:"POST",body:ReadableStream.from([new Uint8Array([65])])});
throws("TypeError",()=>new Request(original,{method:"TRACE"}));
check(!original.bodyUsed && !original.body.locked,"invalid method does not transfer source Request body");
check(await original.text() === "A","source body remains usable");
`

func TestRequestInputContract(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, requestInputContract)
}

func TestRequestInputsReachNativePolicyAndTransport(t *testing.T) {
	var admissions, sent atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		data, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s:%s:%s", r.Method, r.RequestURI, data)
	}))
	defer server.Close()
	profile := stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{
		Client: server.Client(),
		Allow: func(r *http.Request) error {
			admissions.Add(1)
			if "http://"+r.URL.Host != server.URL || r.Method != "patch" || r.URL.Path != "/☃" {
				return fmt.Errorf("unexpected validated request %s %s", r.Method, r.URL)
			}
			return nil
		},
	}}}
	executeWebBasics(t, profile, fmt.Sprintf(`
const request = new Request(%q+"/a/../☃?x=a b#ignored-on-wire",{method:"patch",body:"payload"});
Object.defineProperties(request,{url:{get(){throw Error("shadow URL used by fetch");}},method:{get(){throw Error("shadow method used by fetch");}}});
check(await(await fetch(request)).text() === %q,"native policy and transport receive normalized private metadata");
`, server.URL, "patch:/%E2%98%83?x=a%20b:payload"))
	if admissions.Load() != 1 || sent.Load() != 1 {
		t.Fatalf("admissions=%d sent=%d", admissions.Load(), sent.Load())
	}
}
