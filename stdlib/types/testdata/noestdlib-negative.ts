import {readFileSync, readdirSync} from "node:fs";
import {exec, execFile, spawnSync} from "node:child_process";
// @ts-expect-error spawn belongs to noe:process, not node:child_process
import {spawn} from "node:child_process";
import {ReadableStream} from "node:stream/web";
import {connect} from "noe:sockets";
import {open} from "noe:fs-handle";
import {subscribe} from "noe:signals";
import {MessagePort, Worker as NodeWorker} from "node:worker_threads";
import bufferDefault from "node:buffer";
import dns from "node:dns";
import dnsPromises from "node:dns/promises";
import {lookup as lookupCallback, resolve4 as resolve4Callback} from "node:dns";
import {resolve4} from "node:dns/promises";

// The implemented file API returns text only when an encoding is supplied.
// @ts-expect-error encoded reads are text, not bytes
const bytes: Uint8Array = readFileSync("file.txt", "utf8");
const text: string = readFileSync("file.txt", "utf8");

// NOE accepts only a path for directory listing; Node Dirent options are not implemented.
// @ts-expect-error withFileTypes and Dirent are unsupported
readdirSync(".", {withFileTypes: true});

// BYOB readers are deliberately not implemented.
const stream = new ReadableStream<Uint8Array>();
// @ts-expect-error mode: "byob" is rejected by NOE's stream implementation
stream.getReader({mode: "byob"});

// Only TCP and Unix stream sockets are implemented.
// @ts-expect-error UDP sockets are unsupported
connect({network: "udp", host: "127.0.0.1", port: 53});

// Positional file access opens existing files for reading or read/write.
// @ts-expect-error create/truncate modes are not implemented
void open("file.txt", {mode: "w"});

// Signal support is platform-dependent and the public name set is explicit.
// @ts-expect-error SIGQUIT is not part of the supported subscription surface
subscribe("SIGQUIT");

// The declarations do not imply support for general Node net or Dirent APIs.
// @ts-expect-error NOE does not register node:net
import type {Socket} from "node:net";
void (undefined as unknown as Socket);

// NOE supports base64 but not Node's base64url encoding extension.
// @ts-expect-error base64url is not implemented by Buffer
Buffer.from("YQ", "base64url");

// Runtime ports are created by MessageChannel and cannot be constructed directly.
// @ts-expect-error MessagePort has no public constructor
new MessagePort();

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

const broadOptions: import("node:child_process").ExecOptions = {encoding: "buffer"};
// @ts-expect-error ExecOptions.encoding can be text or buffer
void execFile("tool", [], broadOptions).then(({stdout}) => stdout.trim());

const optionalBufferOptions: {encoding?: "buffer"} = {};
// @ts-expect-error optional buffer mode may return text or bytes
void exec("tool", optionalBufferOptions).then(({stdout}) => stdout.trim());
