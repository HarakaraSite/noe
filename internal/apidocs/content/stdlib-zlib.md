# node:zlib

### API information

Reference: `node:zlib`; aliases: `zlib`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/compress.go and internal/stdlib/compress_stream.go. Reading this reference evaluates no target module and makes no provider request.

Compresses and decompresses byte data with synchronous and callback forms, and provides Web CompressionStream/DecompressionStream globals. The create* helpers return Web Streams rather than Node event-based streams.

### Exports

`constants`, `createDeflate`, `createDeflateRaw`, `createGunzip`, `createGzip`, `createInflate`, `createInflateRaw`, `default`, `deflate`, `deflateRaw`, `deflateRawSync`, `deflateSync`, `gunzip`, `gunzipSync`, `gzip`, `gzipSync`, `inflate`, `inflateRaw`, `inflateRawSync`, `inflateSync`, `unzip`, `unzipSync`

The create* helpers produce Web Compression/Decompression streams. Synchronous functions return bytes; callback functions receive an error and bytes. Supported formats are "gzip", "deflate", and "deflate-raw".

### Compression and decompression streams

Both stream classes expose readable and writable sides. DecompressionStream produces Uint8Array chunks incrementally, including before compressed-input EOF. Consume the readable side while writing: a slow or absent reader can hold write() and close() promises pending. Piping drives both sides together:

~~~js
import {createReadStream, createWriteStream} from "noe:fs";

await createReadStream("archive.gz")
  .pipeThrough(new DecompressionStream("gzip"))
  .pipeTo(createWriteStream("archive.txt"));
~~~

Cancelling the readable side stops decoding and errors the writable side; aborting the writable side errors the readable side. Invalid or truncated compressed input and checksum failures error the stream. Incremental decoding may deliver a prefix before a later validation error, so wait for successful completion before treating the full result as valid.

DecompressionStream retains the existing 1 GiB decompressed-output limit, applied cumulatively across the entire stream. Exceeding it raises RangeError. Runtime shutdown or closing the execution's Bindings stops pending decoding work.

Go hosts using Attach must drive the supplied Loop. Managed Runtime/Execute handles this work as part of execution.

### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
