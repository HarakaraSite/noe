package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const responseContract = `
const response = new Response("hello", {status: 201, statusText: "Created", headers: {"x-test": "yes"}});
check(response.status === 201 && response.statusText === "Created" && response.ok && response.type === "default", "constructed response metadata");
check(response.url === "" && !response.redirected && response.headers === response.headers, "constructed response URL and stable headers");
for (const name of ["status", "statusText", "type", "url", "redirected", "ok", "headers"]) {
  check(!Reflect.set(response,name,"changed") && !Object.hasOwn(response,name), "readonly private response " + name);
  throws("TypeError",()=>Object.getOwnPropertyDescriptor(Response.prototype,name).get.call({}));
}
check(Object.prototype.toString.call(response) === "[object Response]" && Response.length === 0, "Response interface metadata");
for (const status of [0, 101, 103, 199, 600, 700, -1, NaN, Infinity]) throws("RangeError",()=>new Response(null,{status}));
for (const [status,want] of [[200.9,200],[65736,200],[-65336,200],[599,599]]) check(new Response(null,{status}).status === want, "unsigned short conversion");
throws("TypeError",()=>new Response(null,{status:200n}));
for (const statusText of ["a\rb","a\nb","\0","\x1f","\x7f","☃",Symbol()]) throws("TypeError",()=>new Response(null,{statusText}));
check(new Response(null,{statusText:" \tÿ "}).statusText === " \tÿ ", "reason phrase whitespace and obs-text retained");
for (const status of [204,205,304]) {
  for (const body of ["",new Uint8Array(0),new ReadableStream({},{highWaterMark:0})]) throws("TypeError",()=>new Response(body,{status}));
  const empty = new Response(null,{status});
  check(empty.body === null && await empty.text() === "" && !empty.bodyUsed && await empty.text() === "", "null body stays reusable");
}
for (const value of [1,"x",false,Symbol()]) throws("TypeError",()=>new Response(null,value));
check(new Response(undefined,null).body === null,"nullable body/init defaults");
let unexpectedURL = false;
check(new Response(null,{get url(){unexpectedURL=true;throw Error("nonstandard url read");}}).url === "" && !unexpectedURL,"ResponseInit ignores nonstandard url");
const order=[];
new Response({toString(){order.push("body");return "payload";}},{
 get headers(){order.push("headers");return [[{toString(){order.push("name");return "x";}},{toString(){order.push("value");return "v";}}]];},
 get status(){order.push("status");return {valueOf(){order.push("number");return 200;}};},
 get statusText(){order.push("statusText");return {toString(){order.push("text");return "OK";}};},
});
check(order.join() === "body,headers,name,value,status,number,statusText,text","Response argument and dictionary conversion order");
throws("RangeError",()=>new Response(null,{status:700,headers:{" bad":"value"}}));
const source=new Uint8Array([65]);const snapshotted=new Response(source);source[0]=66;
check(await snapshotted.text() === "A","response snapshots byte source");
const original = new Response("cloned",{status:202,headers:{"set-cookie":"a=1"}});
Object.defineProperties(original,{status:{get(){throw Error("shadow status");}},headers:{get(){throw Error("shadow headers");}}});
const cloned=original.clone();check(cloned.status===202 && cloned.headers.getSetCookie()[0]==="a=1" && await cloned.text()==="cloned","clone reads private response metadata");
check(await original.text()==="cloned","clone preserves original body");
throws("TypeError",()=>original.clone());
throws("TypeError",()=>Response.prototype.clone.call({}));
const stream=ReadableStream.from([new Uint8Array([65])]);const reader=stream.getReader();
throws("TypeError",()=>new Response(stream));reader.releaseLock();
await stream.getReader().read();throws("TypeError",()=>new Response(stream));
const error=Response.error();
check(error.status===0 && error.statusText==="" && error.type==="error" && !error.ok && error.body===null && error.url==="" && !error.redirected,"network error response");
for(const value of [error,error.clone(),Response.redirect("https://EXAMPLE.test:00443/a/../☃#part"),Response.redirect("https://example.test/").clone()]) {
  throws("TypeError",()=>value.headers.set("x-test","value"));
  throws("TypeError",()=>value.headers.append("x-test","value"));
  throws("TypeError",()=>value.headers.delete("absent"));
}
const redirect=Response.redirect("https://EXAMPLE.test:00443/a/../☃#part",307);
check(redirect.status===307 && redirect.type==="default" && redirect.headers.get("location")==="https://example.test/%E2%98%83#part" && redirect.body===null && redirect.url==="","redirect URL serialization");
for (const status of [301,302,303,307,308,65838]) check(Response.redirect("https://example.test/",status).status === (status%65536),"redirect allowed statuses");
for (const status of [0,200,304,305,306,309,NaN]) throws("RangeError",()=>Response.redirect("https://example.test/",status));
for (const call of [()=>Response.redirect(),()=>Response.redirect("relative"),()=>Response.redirect(Symbol()),()=>Response.redirect("invalid",200)]) throws("TypeError",call);
const json=Response.json({text:"☃"},{status:201,headers:{"x-test":"yes"}});
check(json.type==="default" && json.status===201 && json.headers.get("content-type")==="application/json" && (await json.json()).text==="☃","JSON response bytes and headers");
json.headers.set("x-test","mutable");
check(Response.json(null,{headers:{"content-type":"custom"}}).headers.get("content-type")==="custom","JSON preserves explicit content type");
for(const data of [undefined,()=>{},Symbol(),1n]) throws("TypeError",()=>Response.json(data));
throws("TypeError",()=>Response.json());
throws("TypeError",()=>Response.json({}, {status:204}));
const circular={};circular.self=circular;throws("TypeError",()=>Response.json(circular));
const jsonOrder=[];Response.json({toJSON(){jsonOrder.push("json");return {}; }},{get headers(){jsonOrder.push("headers");},get status(){jsonOrder.push("status");},get statusText(){jsonOrder.push("statusText");}});
check(jsonOrder.join()==="headers,status,statusText,json","JSON init converts before serialization");
`

func TestResponseContract(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, responseContract)
}
