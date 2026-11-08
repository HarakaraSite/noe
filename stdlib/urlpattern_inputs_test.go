package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const urlPatternInputContract = `
const route = new URLPattern({pathname:"/books/:id"});
const result = route.exec({pathname:"/books/42"});
check(result && result.pathname.groups.id === "42" && result.protocol.input === "", "object component input");
check(!route.test({pathname:"/books/42/pages"}) && route.test("https://example.test/books/42"), "object/string route matching");
check(!route.test({href:"https://example.test/books/42"}), "unknown href is not a URLPatternInit component");
const base = "https://u:p@EXAMPLE.com:8443/a/file?old#base";
const based = new URLPattern({pathname:"books/:id",baseURL:base});
check([based.protocol,based.username,based.password,based.hostname,based.port,based.pathname,based.search,based.hash].join("|") === "https|*|*|example.com|8443|/a/books/:id|*|*", "pattern base inheritance");
check(based.test({pathname:"books/7",baseURL:base}) && based.test("https://example.com:8443/a/books/7?any#value"), "relative dictionary resolution");
check(!based.test("https://example.com/a/books/7"), "base port is constrained");
const fromString = new URLPattern("books/:id",base);
const stringMatch = fromString.exec("books/7",base);
check(stringMatch.pathname.groups.id === "7" && stringMatch.inputs.join("|") === "books/7|"+base, "relative string pattern and result inputs");
check(new URLPattern({pathname:"../books/:id",baseURL:base}).pathname === "/books/:id", "relative dot segments");
const emptyString = new URLPattern("",base);
check([emptyString.pathname,emptyString.search,emptyString.hash].join("|") === "/a/|*|*" && emptyString.test("https://example.com:8443/a/"), "empty constructor string resolves the base directory");
check(new URLPattern("?new",base).pathname === "/a/file" && new URLPattern("#new",base).search === "old", "relative query and hash preserve earlier base components");
const fragment = new URLPattern("https://example.test/x#h");
check(fragment.search === "" && !fragment.test("https://example.test/x?unexpected#h") && fragment.test("https://example.test/x#h"), "fragment transition constrains omitted search");
check(new URLPattern("https://example.test?x").pathname === "/" && new URLPattern("https://example.test#h").pathname === "/", "absolute query and hash initialize special-scheme pathname");
check(new URLPattern("https://example.test").pathname === "*", "origin-only constructor retains pathname wildcard");
const usernameOnly = new URLPattern("https://user@example.test/x");
check(usernameOnly.password === "*" && usernameOnly.test("https://user:secret@example.test/x"), "unspecified password remains a wildcard");
const relativeSlashes = new URLPattern("//other.test/x","https://example.test/a/");
check(relativeSlashes.hostname === "example.test" && relativeSlashes.pathname === "//other.test/x" && relativeSlashes.test("https://example.test//other.test/x") && !relativeSlashes.test("https://other.test/x"), "relative double slash is a same-origin pathname");
check(new URL("https://example.test/a//b/../c").pathname === "/a//c", "shared path normalization preserves empty segments");

const any = new URLPattern();
const inherited = any.exec({baseURL:base,hash:"#changed"});
check([inherited.protocol.input,inherited.username.input,inherited.password.input,inherited.hostname.input,inherited.port.input,inherited.pathname.input,inherited.search.input,inherited.hash.input].join("|") === "https|u|p|example.com|8443|/a/file|old|changed", "URL input inherits credentials and less specific components");
const overridden = any.exec({baseURL:base,hostname:"other.test"});
check([overridden.protocol.input,overridden.username.input,overridden.password.input,overridden.hostname.input,overridden.port.input,overridden.pathname.input,overridden.search.input,overridden.hash.input].join("|") === "https|||other.test||||", "hostname override blocks more specific inheritance");
const scheme = any.exec({baseURL:base,protocol:"http:"});
check(scheme.protocol.input === "http" && scheme.hostname.input === "" && scheme.pathname.input === "", "protocol override");
const credentials = new URLPattern({baseURL:base});
check(credentials.username === "*" && credentials.password === "*" && credentials.search === "old" && credentials.hash === "base", "pattern never inherits credentials");
check(any.exec().inputs.length === 1 && Object.keys(any.exec().inputs[0]).length === 0 && any.test(null), "empty default input dictionary");
const url = new URL("https://example.test/books/42?q#h");
const urlMatch = route.exec(url);
check(urlMatch.pathname.groups.id === "42" && urlMatch.inputs[0].protocol === "https:" && !Object.hasOwn(urlMatch.inputs[0],"href"), "URL object dictionary conversion");

const typed = {pathname:{toString(){return "/books/42";}},extra:"ignored"};
const converted = route.exec(typed);
check(converted.inputs[0] !== typed && converted.inputs[0].pathname === "/books/42" && !Object.hasOwn(converted.inputs[0],"extra"), "converted dictionary snapshot");
const order = [];
const init = {};
for (const key of ["username","search","protocol","port","pathname","password","hostname","hash","baseURL"]) {
  Object.defineProperty(init,key,{get(){order.push(key);return undefined;}});
}
new URLPattern(init,{get ignoreCase(){order.push("ignoreCase");return false;}});
check(order.join() === "baseURL,hash,hostname,password,pathname,port,protocol,search,username,ignoreCase", "Web IDL dictionary order before initialization");
Object.defineProperty(Object.prototype,"pathname",{configurable:true,get(){throw Error("default dictionary read prototype");}});
try {const empty=new URLPattern();check(empty.test() && empty.exec(null).pathname.input === "", "null/undefined use empty dictionary");}
finally {delete Object.prototype.pathname;}

const insensitive = new URLPattern({pathname:"/Books/:id",username:"ADMIN"},{ignoreCase:true});
check(insensitive.test({pathname:"/books/7",username:"ADMIN"}) && !insensitive.test({pathname:"/books/7",username:"admin"}), "ignoreCase applies to pathname, not credentials");
check(new URLPattern({hostname:"UPPER.Example"}).hostname === "upper.example", "hostname normalization");
check(new URLPattern({protocol:"https",port:"443"}).port === "", "default port normalization");
check(any.exec({protocol:"https:",port:"00443",search:"?a b#c",hash:"#a b"}).port.input === "", "input port conversion");
check(new URLPattern({pathname:"/a b/:id"}).test({pathname:"/a b/42"}), "path encoding without splitting components");
check(new URLPattern({pathname:"/x\ud800"}).pathname === "/x%EF%BF%BD", "USVString conversion");
check(new URLPattern({pathname:"/x/:id(\\d+)"}).hasRegExpGroups && !route.hasRegExpGroups, "regexp metadata");
check(any.exec({pathname:"/literal"}).pathname.groups["0"] === "/literal", "wildcard capture");
const named = new URLPattern({pathname:"/:__proto__"}).exec({pathname:"/safe"}).pathname.groups;
check(Object.hasOwn(named,"__proto__") && named.__proto__ === "safe" && Object.getPrototypeOf(named) === Object.prototype, "record keys are data properties");

check(!Reflect.set(route,"pathname","/other") && Object.keys(route).length === 0, "readonly private pattern state");
route.exec = () => null;
check(route.test({pathname:"/books/42"}), "test uses private matcher");
delete route.exec;
check(URLPattern.length === 0 && URLPattern.prototype.test.length === 0 && URLPattern.prototype.exec.length === 0, "optional argument arity");
check(Object.prototype.toString.call(route) === "[object URLPattern]", "interface tag");
throws("TypeError",()=>URLPattern.prototype.test.call({},"https://example.test"));
throws("TypeError",()=>Object.getOwnPropertyDescriptor(URLPattern.prototype,"pathname").get.call({}));
throws("DataCloneError",()=>structuredClone(route));
for (const call of [()=>new URLPattern("/relative"),()=>new URLPattern({pathname:"/x"},base),()=>route.exec({pathname:"/x"},base),()=>new URLPattern({baseURL:"bad"}),()=>new URLPattern({port:"65536"}),()=>new URLPattern(Symbol()),()=>route.test(Symbol()),()=>new URLPattern({},undefined,1)]) throws("TypeError",call);
check(route.exec("not an absolute URL") === null && route.exec({baseURL:"bad"}) === null && any.exec({port:"65536"}) === null && any.exec({baseURL:"https://bad host/"}) === null, "match parse failures return null");
const marker = {};
try {route.exec({get pathname(){throw marker;}});throw Error("getter failure hidden");} catch(error){check(error === marker,"argument conversion exceptions propagate");}
`

func TestURLPatternInputContract(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, urlPatternInputContract)
}
