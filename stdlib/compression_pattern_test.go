package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestCompressionAndURLPatternPublicIntegration(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, `
const base = "https://example.test/api/";
const route = new URLPattern({pathname:"books/:id",baseURL:base});
const match = route.exec({pathname:"books/42",baseURL:base});
check(match && match.pathname.groups.id === "42", "route parameters from object input");
const text = "book="+match.pathname.groups.id+" 😀";
const data = new TextEncoder().encode(text);
const backing = new Uint8Array(data.length+4);backing.fill(255);backing.set(data,2);
for (const format of ["gzip","deflate","deflate-raw"]) {
  const compressor = new CompressionStream(format), decompressor = new DecompressionStream(format);
  check(!Reflect.set(compressor,"readable",null) && !Reflect.set(decompressor,"writable",null), "readonly compression endpoints");
  throws("DataCloneError",()=>structuredClone(compressor));
  throws("DataCloneError",()=>structuredClone(decompressor));
  const compressed = await new Response(ReadableStream.from([new DataView(backing.buffer,2,data.length)]).pipeThrough(compressor)).bytes();
  const decoded = await new Response(ReadableStream.from([compressed]).pipeThrough(decompressor)).text();
  check(decoded === text, "compressed route body round-trip and input view boundaries: "+format);
}
throws("TypeError",()=>new DecompressionStream("unknown"));
`)
}
