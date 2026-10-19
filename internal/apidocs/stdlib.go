package apidocs

// stdlibTopics documents the modules registered by the NOE-maintained
// standard library, based on go-quickjs v0.27.0, and the globals installed by NOE. Bare aliases are
// limited to modulemap.standardLibraryBareModules; a reserved node: prefix
// does not imply that an unregistered Node module exists.
//
// Source basis: internal/stdlib, its UPSTREAM.md, internal/modulemap/builtins.go, and
// internal/runtime/stdlib.go. Export inventories were confirmed against the
// NOE-maintained implementation and Object.keys(await import(...)) in NOE.
func stdlibTopics() []topicSpec {
	return []topicSpec{
		{ID: "stdlib", Kind: "index", Summary: "The 26 built-in Node-style modules installed by NOE, their scope, and important runtime differences.", File: "stdlib.md"},
		{ID: "globals", Kind: "global", Summary: "Global APIs supplied by the pinned go-quickjs engine and NOE-maintained standard library, with streams, network, server, abort, and timer details.", File: "stdlib-globals.md"},
		{ID: "node:assert", Kind: "module", Summary: "Synchronous assertion helpers for checking values and thrown errors.", File: "stdlib-assert.md", Aliases: []string{"assert"}},
		{ID: "node:async_hooks", Kind: "module", Summary: "Tracks async execution context for values that must follow work across promises and timers.", File: "stdlib-async-hooks.md", Aliases: []string{"async_hooks"}},
		{ID: "node:buffer", Kind: "module", Summary: "Provides byte-oriented Buffer helpers and the Buffer global.", File: "stdlib-buffer.md", Aliases: []string{"buffer"}},
		{ID: "node:child_process", Kind: "module", Summary: "Runs operating-system programs to completion and returns captured output.", File: "stdlib-child_process.md", Aliases: []string{"child_process"}},
		{ID: "node:crypto", Kind: "module", Summary: "Offers hashing, HMAC, random values, key derivation, and a limited Web Crypto surface.", File: "stdlib-crypto.md", Aliases: []string{"crypto"}},
		{ID: "node:dns", Kind: "module", Summary: "Resolves names using the host system resolver and exposes Node-style method names as promise-returning functions.", File: "stdlib-dns.md", Aliases: []string{"dns"}},
		{ID: "node:dns/promises", Kind: "module", Summary: "Promise-oriented DNS resolver module.", File: "stdlib-dns-promises.md", Aliases: []string{"dns/promises"}},
		{ID: "node:events", Kind: "module", Summary: "Provides a small EventEmitter implementation and a promise helper.", File: "stdlib-events.md", Aliases: []string{"events"}},
		{ID: "node:fs", Kind: "module", Summary: "Reads and writes files synchronously or as Web Streams.", File: "stdlib-fs.md", Aliases: []string{"fs"}},
		{ID: "node:fs/promises", Kind: "module", Summary: "Provides promise-returning whole-file and metadata operations.", File: "stdlib-fs-promises.md", Aliases: []string{"fs/promises"}},
		{ID: "node:http", Kind: "module", Summary: "Provides an HTTP server backed by the Web Request and Response APIs.", File: "stdlib-http.md", Aliases: []string{"http"}},
		{ID: "node:os", Kind: "module", Summary: "Reports selected host system information.", File: "stdlib-os.md", Aliases: []string{"os"}},
		{ID: "node:path", Kind: "module", Summary: "Computes path strings without reading the filesystem.", File: "stdlib-path.md", Aliases: []string{"path"}},
		{ID: "node:path/posix", Kind: "module", Summary: "The explicit POSIX path-flavor module.", File: "stdlib-path-posix.md", Aliases: []string{"path/posix"}},
		{ID: "node:path/win32", Kind: "module", Summary: "The explicit Windows path-flavor module.", File: "stdlib-path-win32.md", Aliases: []string{"path/win32"}},
		{ID: "node:process", Kind: "module", Summary: "Exports the process object as the module default and as the global process.", File: "stdlib-process.md", Aliases: []string{"process"}},
		{ID: "node:querystring", Kind: "module", Summary: "Parses and serializes legacy query-string text.", File: "stdlib-querystring.md", Aliases: []string{"querystring"}},
		{ID: "node:stream/web", Kind: "module", Summary: "Exports Web Streams classes and strategies, including text encoding/decoding streams.", File: "stdlib-stream-web.md", Aliases: []string{"stream/web"}},
		{ID: "node:string_decoder", Kind: "module", Summary: "Decodes byte chunks without losing a UTF-8 character split across chunk boundaries.", File: "stdlib-string-decoder.md", Aliases: []string{"string_decoder"}},
		{ID: "node:timers", Kind: "module", Summary: "Exports callback-style timer functions.", File: "stdlib-timers.md", Aliases: []string{"timers"}},
		{ID: "node:timers/promises", Kind: "module", Summary: "Provides Promise and async-iterator timer forms.", File: "stdlib-timers-promises.md", Aliases: []string{"timers/promises"}},
		{ID: "node:url", Kind: "module", Summary: "Exposes Web URL and URLSearchParams plus a small set of URL and file-URL helpers.", File: "stdlib-url.md", Aliases: []string{"url"}},
		{ID: "node:util", Kind: "module", Summary: "Provides selected formatting, inspection, type, inheritance, and callback/promise conversion helpers.", File: "stdlib-util.md", Aliases: []string{"util"}},
		{ID: "node:vm", Kind: "module", Summary: "Creates JavaScript contexts and compiles or runs scripts inside the current QuickJS runtime.", File: "stdlib-vm.md", Aliases: []string{"vm"}},
		{ID: "node:worker_threads", Kind: "module", Summary: "Runs JavaScript in additional QuickJS runtimes and exchanges structured-clone messages through ports.", File: "stdlib-worker-threads.md", Aliases: []string{"worker_threads"}},
		{ID: "node:zlib", Kind: "module", Summary: "Compresses and decompresses byte data with synchronous and promise forms, and provides Web CompressionStream/DecompressionStream globals.", File: "stdlib-zlib.md", Aliases: []string{"zlib"}},
	}
}
