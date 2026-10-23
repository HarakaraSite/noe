# node:zlib

### API information

Reference: `node:zlib`; aliases: `zlib`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/compress.go. Reading this reference evaluates no target module and makes no provider request.

Compresses and decompresses byte data with synchronous and promise forms, and provides Web CompressionStream/DecompressionStream globals. This is a byte API, not Node's callback or event-based stream API.

### Exports

`constants`, `createDeflate`, `createDeflateRaw`, `createGunzip`, `createGzip`, `createInflate`, `createInflateRaw`, `default`, `deflate`, `deflateRaw`, `deflateRawSync`, `deflateSync`, `gunzip`, `gunzipSync`, `gzip`, `gzipSync`, `inflate`, `inflateRaw`, `inflateRawSync`, `inflateSync`, `unzip`, `unzipSync`

The create* helpers produce Web Compression/Decompression streams. Promise operations return bytes; use Web Streams for incremental processing.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
