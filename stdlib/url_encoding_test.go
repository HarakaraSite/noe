package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const urlEncodingContract = `
const encoded = new URL(" \u0000HTTPS://u ser:p@ss@EXAMPLE.test:00443/a b/./c/../☃?x=a b'\"<>#f " + String.fromCharCode(96));
check(encoded.href === "https://u%20ser:p%40ss@example.test/a%20b/%E2%98%83?x=a%20b%27%22%3C%3E#f%20%60", "constructor component encoding");
check(new URL("https://x/a/%2E%2e/b/%2e./c//d").href === "https://x/c//d", "encoded dot segments and empty segments");
check(new URL("https://x/\ud800?q=\udc00#\ud800").href === "https://x/%EF%BF%BD?q=%EF%BF%BD#%EF%BF%BD", "USVString in all components");
check(new URL("https:foo","https://x/a/").href === "https://x/a/foo", "same special scheme relative resolution");
check(new URL("https:\\\\EXAMPLE.test\\a\\..\\b").href === "https://example.test/b", "special reverse solidus normalization");
check(new URL("foo://EXAMPLE.test/a b?q='").href === "foo://EXAMPLE.test/a%20b?q='", "non-special host and query encoding");
const noAuthority = new URL("foo:/a/..//b");
check(noAuthority.href === "foo:/.//b" && noAuthority.host === "" && noAuthority.port === "" && new URL(noAuthority.href).href === noAuthority.href, "serialization does not turn a path into an authority");
check(new URL("https://x/%2f/%ZZ").pathname === "/%2f/%ZZ", "preserve existing percent escapes");

const url = new URL("https://x/?a=one");
const params = url.searchParams;
url.search = "?a=two&bad=%E0%A4%A&plus=%2B+";
check(url.searchParams === params && params.get("a") === "two" && params.get("bad") === "�%A" && params.get("plus") === "+ ", "same query object after search and replacement decoding");
url.href = "https://other.test/??name=value";
check(url.searchParams === params && params.get("?name") === "value", "same query object after href and literal leading question mark");
params.set("q","a b~☃");
check(url.href === "https://other.test/?%3Fname=value&q=a+b%7E%E2%98%83", "form serialization rewrites URL query");
params.toString = () => "injected=yes";
params.set("q","safe");
check(url.search.includes("q=safe") && !url.search.includes("injected"), "parameter updates use private serializer");
url.search = "?";url.hash = "#";
check(url.href === "https://other.test/?#" && url.search === "" && url.hash === "", "empty component markers remain distinct from absence");
params.delete("absent");
check(url.href === "https://other.test/#", "empty form serialization removes query marker");
url.hash = "";
check(url.href === "https://other.test/", "empty hash assignment removes marker");
throws("TypeError",()=>new URLSearchParams(["ab"]));
check(new URLSearchParams([new String("ab")]).toString() === "a=b", "pair sequence requires an iterable object");
const completedParams = new URLSearchParams("a=1"), completedIterator = completedParams.entries();
completedIterator.next();check(completedIterator.next().done,"iterator completes");
completedParams.append("b","2");check(completedIterator.next().value[0] === "b","Web IDL iterator observes a later append after done");

const setters = new URL("https://x:8080/a");
setters.pathname = "a/%2e%2e/b";check(setters.pathname === "/b", "relative pathname setter has a root before dot removal");
setters.pathname = "a?b#c";setters.port = "00443";
check(setters.href === "https://x/a%3Fb%23c", "path setter and default port normalization");
setters.port = "65536";check(setters.port === "", "invalid port setter leaves state");
setters.port = "123abc";check(setters.port === "123", "port override accepts numeric prefix");
setters.host = "other.test:bad";check(setters.hostname === "other.test" && setters.port === "123", "host setter retains port on failed port parse");
setters.hostname = "wrong.test:12";check(setters.hostname === "other.test", "hostname setter rejects port syntax");
setters.protocol = "foo:";check(setters.protocol === "https:", "protocol setter preserves special status");
setters.username = "u:@/";setters.password = "p:@/";
check(setters.username === "u%3A%40%2F" && setters.password === "p%3A%40%2F", "credential setter encoding");
const opaque = new URL("data:hi there ☃?q=x#h");opaque.pathname = "/ignored";opaque.username = "ignored";
check(opaque.pathname === "hi there %E2%98%83" && opaque.username === "", "opaque setters preserve opaque path");
check(new URL("#new",opaque).hash === "#new", "opaque fragment resolution");
throws("TypeError",()=>new URL("relative",opaque));
// Current primary URL Standard encodes the last space before query/fragment.
const opaqueSpace = new URL("data:abc  ?q#h");
check(opaqueSpace.pathname === "abc %20", "primary opaque-space encoding");
opaqueSpace.searchParams.delete("q");opaqueSpace.hash = "";
check(opaqueSpace.href === "data:abc %20", "query removal preserves the primary opaque path");

for (const call of [()=>new URL(),()=>URL.parse(),()=>URL.canParse(),()=>new URL(Symbol()),()=>URL.parse(Symbol()),()=>URL.canParse(Symbol()),()=>new URL("https://x:65536/"),()=>new URL("https://x:1bad/")]) throws("TypeError",call);
check(URL.parse("bad") === null && !URL.canParse("bad") && URL.canParse("https://x/☃"), "parse failure versus conversion");
const marker = {};
try {new URLSearchParams([["short"],[{toString(){throw marker;}},"value"]]);throw Error("sequence conversion skipped");} catch(error){check(error === marker,"complete sequence conversion precedes constructor pair-length checks");}
try { URL.canParse({toString(){throw marker;}});throw Error("lost conversion error"); } catch(error){check(error === marker,"canParse conversion error propagates");}
const reentrant = new URL("https://old.test/original");
reentrant.pathname = {toString(){reentrant.href="https://new.test/base";return "changed";}};
check(reentrant.href === "https://new.test/changed", "setter uses current URL after argument conversion");
reentrant.protocol = {toString(){reentrant.href="http://new.test/base";return "https:";}};
check(reentrant.href === "https://new.test/base", "protocol setter observes reentrant conversion");
const order=[];
new URL({toString(){order.push("input");return "https://x/";}},{toString(){order.push("base");return "https://base/";}});
check(order.join() === "input,base", "argument conversion order");
Object.defineProperty(encoded,"href",{get(){throw Error("public href was read");}});
check(URL.prototype.toString.call(encoded).startsWith("https://u%20ser") && URL.prototype.toJSON.call(encoded) === URL.prototype.toString.call(encoded), "private stringification");
throws("TypeError",()=>URL.prototype.toString.call({}));
throws("TypeError",()=>Object.getOwnPropertyDescriptor(URL.prototype,"href").get.call({}));
check(Object.prototype.toString.call(url) === "[object URL]" && !Reflect.set(url,"searchParams",null), "URL tag and readonly SameObject query");
check(URL.length === 1 && URL.parse.length === 1 && URL.canParse.length === 1, "required URL argument arity");
`

func TestURLEncodingContract(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, urlEncodingContract)
}

func TestURLQueryFetchBody(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, `
const params = new URLSearchParams("x=%E0%A4%A&space=+&bom=%EF%BB%BF");
const request = new Request(new URL("https://example.test/☃"), {method:"POST",body:params});
check(request.url === "https://example.test/%E2%98%83", "Fetch Request accepts normalized URL object");
check(request.headers.get("content-type") === "application/x-www-form-urlencoded;charset=UTF-8", "Fetch form content type");
check(await request.text() === "x=%EF%BF%BD%25A&space=+&bom=%EF%BB%BF", "Fetch form bytes use replacement UTF-8 and retain BOM");
`)
}
