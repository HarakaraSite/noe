import noeFs, {readFileSync, readdirSync, statSync, open as openFile} from "noe:fs";
import processDefault, {exec, execFile, spawnSync, process as processInfo} from "noe:process";
// @ts-expect-error The old Node-prefixed command module was removed.
import {exec as legacyNodeExec} from "node:child_process";
// @ts-expect-error The old Node-prefixed process module was removed.
import {stdin as legacyNodeStdin} from "node:process";
// @ts-expect-error Bare child_process is no longer a reserved NOE module name.
import {exec as legacyBareExec} from "child_process";
// @ts-expect-error Bare process is no longer a reserved NOE module name.
import {stdin as legacyBareStdin} from "process";
// @ts-expect-error Standard I/O is exported from noe:process, not noe:io.
import {stdin as legacyStdin} from "noe:io";
import {ReadableStream} from "node:stream/web";
import {bind, connect, listen, type DatagramAddress, type DatagramSocket} from "noe:sockets";
import {subscribe} from "noe:signals";
import {MessagePort, Worker as NodeWorker} from "node:worker_threads";
import bufferDefault from "node:buffer";
import dns from "node:dns";
import dnsPromises from "node:dns/promises";
import {lookup as lookupCallback, resolve4 as resolve4Callback} from "node:dns";
import {resolve4} from "node:dns/promises";
import {open as openSQLite} from "noe:sqlite";
// @ts-expect-error NOE does not declare Node's built-in SQLite module.
import {DatabaseSync} from "node:sqlite";
import * as sqliteModule from "noe:sqlite";
// @ts-expect-error CryptoKey is an illegal constructor.
new CryptoKey();
// @ts-expect-error A CryptoKey's extractability attribute is readonly.
(undefined as unknown as CryptoKey).extractable = true;
// @ts-expect-error Plain arrays are not Web Crypto BufferSources.
crypto.subtle.digest("SHA-256", [1, 2, 3]);
// @ts-expect-error SharedArrayBuffer is not an accepted Web Crypto buffer.
crypto.subtle.digest("SHA-256", new SharedArrayBuffer(4));
// @ts-expect-error Key usages must use a known Web IDL enum value.
crypto.subtle.importKey("raw", new Uint8Array(16), {name: "HMAC", hash: "SHA-256"}, false, ["unknown"]);
// @ts-expect-error This slice does not implement JWK imports.
crypto.subtle.importKey("jwk", {kty: "oct", k: "AQID"}, {name: "HMAC", hash: "SHA-256"}, false, ["sign"]);
import {createWebSocket, serve as noeServe} from "noe:http";

// HTTP helpers use noe:http; node:http and its bare alias are removed.
// @ts-expect-error NOE does not declare node:http.
import {serve as legacyNodeServe} from "node:http";
// @ts-expect-error NOE does not declare the bare http alias.
import {serve as legacyBareServe} from "http";
// @ts-expect-error noe:http does not export Fetch; use the standard global.
import {fetch as legacyNoeFetch} from "noe:http";
import * as noeHTTP from "noe:http";
// @ts-expect-error noe:http has no default export.
const noeHTTPDefault = noeHTTP.default;

// HTTPS listener credentials are PEM text strings.
// @ts-expect-error cert must be a string containing PEM text.
noeServe({cert: new Uint8Array()}, request => new Response(request.url));
// @ts-expect-error key must be a string containing PEM text.
noeServe({key: 42}, request => new Response(request.url));

// Filesystem imports have moved to one NOE-owned module.
// @ts-expect-error NOE no longer declares node:fs.
import {readFileSync as legacyNodeRead} from "node:fs";
// @ts-expect-error NOE no longer declares node:fs/promises.
import {open as legacyNodeOpen} from "node:fs/promises";
// @ts-expect-error NOE no longer declares the fs alias.
import {readFileSync as legacyBareRead} from "fs";
// @ts-expect-error NOE no longer declares the fs/promises alias.
import {open as legacyBareOpen} from "fs/promises";

// @ts-expect-error EventSource needs a URL.
new EventSource();
// @ts-expect-error EventSource only supports its standard options.
new EventSource("https://example.test/events", {headers: {Authorization: "fixture"}});
// @ts-expect-error EventSource credentials mode is boolean.
new EventSource("https://example.test/events", {withCredentials: "true"});
const events = new EventSource("https://example.test/events");
// @ts-expect-error EventSource state is read-only.
events.readyState = EventSource.CLOSED;
events.onmessage = event => {
  // @ts-expect-error SSE message data is decoded text.
  const bytes: Uint8Array = event.data;
  void bytes;
};

// The implemented file API returns text only when an encoding is supplied.
// @ts-expect-error encoded reads are text, not bytes
const bytes: Uint8Array = readFileSync("file.txt", "utf8");
const text: string = readFileSync("file.txt", "utf8");
// @ts-expect-error mtime is a Date; use mtimeMs for numeric timestamps
const oldTimestampString: string = statSync("file.txt").mtime;

// Directory entry mode must be a boolean.
// @ts-expect-error withFileTypes does not accept strings
readdirSync(".", {withFileTypes: "true"});

// BYOB readers are deliberately not implemented.
const stream = new ReadableStream<Uint8Array>();
// @ts-expect-error mode: "byob" is rejected by NOE's stream implementation
stream.getReader({mode: "byob"});

// connect/listen support TCP, Unix streams, and authenticated TLS streams.
// @ts-expect-error UDP sockets are unsupported
connect({network: "udp", host: "127.0.0.1", port: 53});
// @ts-expect-error UDP sockets use bind(), not the stream listener API.
listen({network: "udp", host: "127.0.0.1", port: 53});
// @ts-expect-error UDP bind requires a string host.
bind({network: "udp", host: 127, port: 0});
// @ts-expect-error UDP bind does not accept TLS credentials.
bind({network: "udp", host: "127.0.0.1", port: 0, cert: "pem"});
declare const datagramSocket: DatagramSocket;
declare const datagramAddress: DatagramAddress;
// @ts-expect-error UDP datagram endpoints are separate from stream addresses.
const streamAddress: import("noe:sockets").Address = datagramAddress;
// @ts-expect-error Datagram sends accept bytes, not text.
datagramSocket.send("text", datagramAddress);
void streamAddress;
// @ts-expect-error TLS-only trust options cannot be supplied to TCP.
connect({network: "tcp", host: "localhost", port: 443, ca: "pem"});
// @ts-expect-error TLS credentials cannot be supplied to Unix sockets.
connect({network: "unix", path: "/tmp/noe.sock", cert: "pem"});
// @ts-expect-error TLS PEM values must be strings.
connect({network: "tls", host: "localhost", port: 443, ca: new Uint8Array()});
// @ts-expect-error TLS listen only accepts server credentials, not client trust options.
listen({network: "tls", host: "127.0.0.1", port: 0, ca: "pem"});
// @ts-expect-error TLS listen does not accept client-only serverName.
listen({network: "tls", host: "127.0.0.1", port: 0, serverName: "localhost"});
// @ts-expect-error TLS listen credentials must be PEM text strings.
listen({network: "tls", host: "127.0.0.1", port: 0, key: 42});

const file = await openFile("file.txt", {flags: "r+"});
// The former noe:fs-handle module remains removed; positional operations are on noe:fs.FileHandle.
// @ts-expect-error noe:fs-handle has no supported declaration
import {open as legacyOpen} from "noe:fs-handle";
const readAtResult: Uint8Array = await file.readAt(0, 1);
const writeAtResult: number = await file.writeAt(0, new Uint8Array([1]));
// Open accepts an options object; positional string flags are rejected.
// @ts-expect-error open options are an object, not positional flags
void openFile("file.txt", "r+");
// @ts-expect-error open flags are limited to the supported set
void openFile("file.txt", {flags: "rs"});
// @ts-expect-error open mode must be a number
void openFile("file.txt", {flags: "r+", mode: "0600"});
// @ts-expect-error read accepts a length, not a Node buffer tuple
void file.read(new Uint8Array(1));
// @ts-expect-error write accepts exactly the data value
void file.write("text", 0);

// Signal support is platform-dependent and the public name set is explicit.
// @ts-expect-error SIGQUIT is not part of the supported subscription surface
subscribe("SIGQUIT");

// The declarations do not imply support for general Node net or Dirent APIs.
// @ts-expect-error NOE does not register node:net
import type {Socket} from "node:net";
void (undefined as unknown as Socket);

async function sqliteNegativeFixture(): Promise<void> {
  const database = await openSQLite(":memory:");
  // @ts-expect-error database metadata is read-only.
  database.readOnly = true;
  // @ts-expect-error closed is controlled by the resource lifecycle.
  database.closed = true;
  // @ts-expect-error query methods are asynchronous.
  const synchronousRow: import("noe:sqlite").SQLiteRow | undefined = database.get("SELECT 1");
  // @ts-expect-error SQL input values do not include symbols.
  await database.run("SELECT ?", [Symbol("invalid")]);
  // @ts-expect-error busyTimeout is measured in milliseconds as a number.
  await openSQLite("db.sqlite", {busyTimeout: "quick"});
  // @ts-expect-error transaction mode must be one of the supported SQLite modes.
  await database.transaction(tx => tx.exec("SELECT 1"), {mode: "serializable"});
  // @ts-expect-error transaction handles expose query methods, not close().
  await database.transaction(tx => tx.close());
  const statement = await database.prepare("SELECT 1");
  // @ts-expect-error statement closed is controlled by the resource lifecycle.
  statement.closed = true;
  // @ts-expect-error row output values are not guaranteed to be strings.
  const stringValue: string = (await statement.get())!.value;
  void [synchronousRow, stringValue];
  await database.close();
}
void sqliteNegativeFixture;
// @ts-expect-error noe:sqlite has no default constructor export.
new sqliteModule.default();

const processMetadataType: typeof process = processInfo;
const defaultNamespaceProcessType: typeof processInfo = processDefault.process;
// @ts-expect-error The default namespace groups exports; metadata is under its named process export.
processDefault.cwd();
// @ts-expect-error The default namespace groups exports; metadata is under its named process export.
processDefault.env;

if (processDefault.stdin) {
  // @ts-expect-error stdin is a byte ReadableStream; use a reader or Response.
  processDefault.stdin.read();
  // @ts-expect-error Web Streams do not expose the old isTTY facade.
  processDefault.stdin.isTTY;
}
if (processDefault.stdout) {
  // @ts-expect-error stdout is a byte WritableStream; use getWriter().write().
  processDefault.stdout.write("text");
  // @ts-expect-error Web Streams do not expose the old isTTY facade.
  processDefault.stdout.isTTY;
}
if (processDefault.stderr) {
  // @ts-expect-error stderr is a byte WritableStream; use getWriter().write().
  processDefault.stderr.write("text");
}
void [processMetadataType, defaultNamespaceProcessType, legacyNodeExec, legacyNodeStdin, legacyBareExec, legacyBareStdin];

// NOE supports base64 but not Node's base64url encoding extension.
// @ts-expect-error base64url is not implemented by Buffer
Buffer.from("YQ", "base64url");

// Runtime ports are created by MessageChannel and cannot be constructed directly.
// @ts-expect-error MessagePort has no public constructor
new MessagePort();

// Storage is an interface object, but its constructor is illegal.
// @ts-expect-error Storage cannot be constructed.
new Storage();
// @ts-expect-error Storage cannot be called.
Storage();
// @ts-expect-error sessionStorage is a getter-only global.
sessionStorage = localStorage;
// @ts-expect-error localStorage is a getter-only global.
localStorage = sessionStorage;
// @ts-expect-error Storage.length is getter-only.
sessionStorage.length = 1;

// The default node:buffer export contains only Buffer; atob/btoa are named exports.
// @ts-expect-error default buffer export has no atob member
bufferDefault.atob;

// NodeWorker ignores the web Worker `type` option.
// @ts-expect-error type is not part of NOE's Node worker options
new NodeWorker("./worker.mjs", {type: "module"});

// A query A result is an address string, not the object used by lookup().
// @ts-expect-error resolve4 returns strings
resolve4("example.test").then(records => records[0].address);

// @ts-expect-error resolve4 callback records are strings
resolve4Callback("example.test", (_error, records) => records?.[0].address);

// Resolver instances and the promises module expose different methods.
// @ts-expect-error the callback Resolver instance does not expose lookup
new dns.Resolver().lookup("example.test", () => {});

// @ts-expect-error the promises module has no cancel method
dnsPromises.cancel();

// @ts-expect-error promise Resolver instances do not expose lookup
new dnsPromises.Resolver().lookup("example.test");

lookupCallback("", (_error, address) => {
  if (address !== undefined && !Array.isArray(address)) {
    // @ts-expect-error an empty hostname may return null, even after excluding undefined
    address.toUpperCase();
  }
});

dnsPromises.lookup("").then(result => {
  if (result && !Array.isArray(result)) {
    // @ts-expect-error an empty hostname result may have a null address
    result.address.toUpperCase();
  }
});

// Buffer mode returns Uint8Array data, which has no string methods.
// @ts-expect-error buffer output is not a string
void execFile("tool", [], {encoding: "buffer"}).then(({stdout}) => stdout.trim());

// @ts-expect-error buffer output is not a string
void exec("tool", {encoding: "buffer"}).then(({stdout}) => stdout.trim());

// A widened encoding could be either text or buffer at runtime.
const options = {encoding: "buffer"};
// @ts-expect-error widened options produce string | Uint8Array
void execFile("tool", [], options).then(({stdout}) => stdout.trim());
// @ts-expect-error widened options produce string | Uint8Array
void exec("tool", options).then(({stdout}) => stdout.trim());
// @ts-expect-error widened options produce string | Uint8Array
spawnSync("tool", [], options).stdout.trim();

const broadOptions: import("noe:process").ExecOptions = {encoding: "buffer"};
// @ts-expect-error ExecOptions.encoding can be text or buffer
void execFile("tool", [], broadOptions).then(({stdout}) => stdout.trim());

const optionalBufferOptions: {encoding?: "buffer"} = {};
// @ts-expect-error optional buffer mode may return text or bytes
void exec("tool", optionalBufferOptions).then(({stdout}) => stdout.trim());

// @ts-expect-error a controller is not an AbortSignal
AbortSignal.any([new AbortController()]);
// @ts-expect-error characters are not AbortSignals
AbortSignal.any("signal");
// @ts-expect-error any requires an iterable of signals
AbortSignal.any({});
declare const disposalFile: import("noe:fs").FileHandle;
// @ts-expect-error disposal completes asynchronously
const synchronousDisposal: number = disposalFile[Symbol.asyncDispose]();

// Unsupported watch modes and invalid watch argument shapes are excluded.
// @ts-expect-error filesystem watch paths are strings
noeFs.watch(new URL("file:///tmp"));
// @ts-expect-error recursive watching is not supported
noeFs.watch(".", {recursive: true});
// @ts-expect-error watch encoding must be supported
noeFs.watch(".", {encoding: "hex"});
// @ts-expect-error noe:fs exposes no promises sub-object
noeFs.promises;
// @ts-expect-error watching is an async iterator, not an EventEmitter callback API
noeFs.watch(".", (_type, _filename) => {});
// @ts-expect-error watch signals must be genuine AbortSignal values
noeFs.watch(".", {signal: {aborted: false}});
// @ts-expect-error queue size is numeric
noeFs.watch(".", {maxQueue: "large"});
// @ts-expect-error overflow selects a supported policy
noeFs.watch(".", {overflow: "discard"});
// @ts-expect-error buffer filenames are not strings
const wrongWatchIterator: AsyncIterableIterator<{eventType: "change" | "rename"; filename: string | null}> = noeFs.watch(".", "buffer");

// @ts-expect-error global serve is removed; import it from noe:http.
globalThis.serve(request => new Response(request.url));
// @ts-expect-error global upgradeWebSocket is removed; import it from noe:http.
globalThis.upgradeWebSocket(new Request("http://example.test/ws"));
declare const synchronousCloseServer: import("noe:http").HTTPServer;
// @ts-expect-error Serve close returns a promise that must be awaited for completion.
const synchronousServerCloseResult: void = synchronousCloseServer.close();
// @ts-expect-error WebSocket requires a URL or string.
new WebSocket(null);
// @ts-expect-error the standard global constructor accepts only URL and protocols.
new WebSocket("wss://example.test", ["chat.v1"], {headers: {Authorization: "Bearer fixture"}});
// @ts-expect-error protocols are a string or string array, not a custom options object.
new WebSocket("wss://example.test", {headers: {Authorization: "Bearer fixture"}});
// @ts-expect-error custom handshake header values must be strings in typed code.
createWebSocket("wss://example.test", {headers: {Authorization: 42}});
// @ts-expect-error header entries must contain exactly a name and a value.
createWebSocket("wss://example.test", {headers: [["X-Test"]]});
// @ts-expect-error the helper does not expose a JavaScript TLS override.
createWebSocket("wss://example.test", {tls: {rejectUnauthorized: false}});

// @ts-expect-error CustomEvent requires its event type.
new CustomEvent();
const readonlyCustomEvent = new CustomEvent("update", {detail: 1});
// @ts-expect-error Event detail is a readonly attribute.
readonlyCustomEvent.detail = 2;
// @ts-expect-error Inherited event flags are readonly attributes.
readonlyCustomEvent.bubbles = true;
// @ts-expect-error Performance itself is an illegal constructor.
new Performance();
// @ts-expect-error PerformanceEntry is an illegal constructor.
new PerformanceEntry();
// @ts-expect-error PerformanceMeasure is an illegal constructor.
new PerformanceMeasure();
// @ts-expect-error mark requires a name.
performance.mark();
// @ts-expect-error timestamps must be numbers in typed code.
performance.mark("invalid", {startTime: "1"});
// @ts-expect-error options and a legacy end-mark argument cannot be combined.
performance.measure("invalid", {start: 1}, "end");
// @ts-expect-error mark metadata is readonly.
new PerformanceMark("readonly").startTime = 2;
// @ts-expect-error encodeInto requires a Uint8Array destination.
new TextEncoder().encodeInto("a", new Uint16Array(1));
