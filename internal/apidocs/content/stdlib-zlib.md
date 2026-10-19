# node:zlib

Compresses and decompresses byte data with synchronous and promise forms, and provides Web CompressionStream/DecompressionStream globals. This is a byte API, not Node's callback or event-based stream API.

### Exports

`constants`, `createDeflate`, `createDeflateRaw`, `createGunzip`, `createGzip`, `createInflate`, `createInflateRaw`, `default`, `deflate`, `deflateRaw`, `deflateRawSync`, `deflateSync`, `gunzip`, `gunzipSync`, `gzip`, `gzipSync`, `inflate`, `inflateRaw`, `inflateRawSync`, `inflateSync`, `unzip`, `unzipSync`

The create* helpers produce Web Compression/Decompression streams. Promise operations return bytes; use Web Streams for incremental processing.



### Source basis

Pinned implementation: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
