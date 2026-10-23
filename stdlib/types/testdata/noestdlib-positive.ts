import assert, {deepStrictEqual, rejects as assertRejects, strict} from "node:assert";
import {AsyncLocalStorage} from "async_hooks";
import bufferDefault, {Buffer} from "node:buffer";
import {exec, execFile, spawnSync} from "child_process";
import {createHash, randomBytes} from "crypto";
import dns, {lookup as lookupCallback, resolve4 as resolve4Callback} from "node:dns";
import * as dnsPromises from "dns/promises";
import {EventEmitter, once} from "events";
import fs, {readFileSync, statSync} from "node:fs";
import {readFile, readdir} from "fs/promises";
import {serve as nodeServe} from "node:http";
import {platform, cpus} from "os";
import path, {join, posix} from "node:path";
import processModule from "process";
import {parse as parseQuery} from "querystring";
import {ReadableStream as ModuleReadableStream} from "node:stream/web";
import {StringDecoder} from "string_decoder";
import {setTimeout as delay} from "timers/promises";
import {pathToFileURL} from "url";
import {promisify, types as utilTypes} from "util";
import {runInNewContext} from "node:vm";
import {SHARE_ENV as WORKER_SHARE_ENV, Worker as NodeWorker} from "worker_threads";
import {gzipSync} from "zlib";

import {docs} from "noe:docs";
import {stdin, stdout, pipe} from "noe:io";
import {spawn as spawnOwned} from "noe:process";
import {fetch as noeFetch, serve as noeServe} from "noe:http";
import {open} from "noe:fs-handle";
import {connect, listen} from "noe:sockets";
import {subscribe} from "noe:signals";

// Every registered bare name resolves to the same declared surface as its
// node:-prefixed canonical name.
const assertAlias: typeof import("assert") = undefined as unknown as typeof import("node:assert");
const asyncHooksAlias: typeof import("async_hooks") = undefined as unknown as typeof import("node:async_hooks");
const bufferAlias: typeof import("buffer") = undefined as unknown as typeof import("node:buffer");
const childProcessAlias: typeof import("child_process") = undefined as unknown as typeof import("node:child_process");
const cryptoAlias: typeof import("crypto") = undefined as unknown as typeof import("node:crypto");
const dnsAlias: typeof import("dns") = undefined as unknown as typeof import("node:dns");
const dnsPromisesAlias: typeof import("dns/promises") = undefined as unknown as typeof import("node:dns/promises");
const eventsAlias: typeof import("events") = undefined as unknown as typeof import("node:events");
const fsAlias: typeof import("fs") = undefined as unknown as typeof import("node:fs");
const fsPromisesAlias: typeof import("fs/promises") = undefined as unknown as typeof import("node:fs/promises");
const httpAlias: typeof import("http") = undefined as unknown as typeof import("node:http");
const osAlias: typeof import("os") = undefined as unknown as typeof import("node:os");
const pathAlias: typeof import("path") = undefined as unknown as typeof import("node:path");
const pathPosixAlias: typeof import("path/posix") = undefined as unknown as typeof import("node:path/posix");
const pathWin32Alias: typeof import("path/win32") = undefined as unknown as typeof import("node:path/win32");
const processAlias: typeof import("process") = undefined as unknown as typeof import("node:process");
const querystringAlias: typeof import("querystring") = undefined as unknown as typeof import("node:querystring");
const streamWebAlias: typeof import("stream/web") = undefined as unknown as typeof import("node:stream/web");
const stringDecoderAlias: typeof import("string_decoder") = undefined as unknown as typeof import("node:string_decoder");
const timersAlias: typeof import("timers") = undefined as unknown as typeof import("node:timers");
const timersPromisesAlias: typeof import("timers/promises") = undefined as unknown as typeof import("node:timers/promises");
const urlAlias: typeof import("url") = undefined as unknown as typeof import("node:url");
const utilAlias: typeof import("util") = undefined as unknown as typeof import("node:util");
const vmAlias: typeof import("vm") = undefined as unknown as typeof import("node:vm");
const workerAlias: typeof import("worker_threads") = undefined as unknown as typeof import("node:worker_threads");
const zlibAlias: typeof import("zlib") = undefined as unknown as typeof import("node:zlib");

assert.ok(true);
strict.equal(1, "1");
deepStrictEqual({answer: 42}, {answer: 42});
void assertRejects(Promise.resolve("done"));

const context = new AsyncLocalStorage<number>();
const store: number | undefined = context.run(7, () => context.getStore());
const bytes: Uint8Array = Buffer.from("text", "utf8");
const bufferValue: Uint8Array = bufferDefault.Buffer.alloc(4);
const hash: string = createHash("sha256").update(bytes).digest("hex");
const random: Uint8Array = randomBytes(8);
const command: Promise<{stdout: string; stderr: string}> = execFile("git", ["status", "--short"]);
const binaryCommand: Promise<{stdout: Uint8Array; stderr: Uint8Array}> = execFile("git", ["status"], {encoding: "buffer"});
const binaryShell: Promise<{stdout: Uint8Array; stderr: Uint8Array}> = exec("printf test", {encoding: "buffer"});
const widenedOptions = {encoding: "buffer"};
const widenedFileOutput: Promise<{stdout: string | Uint8Array; stderr: string | Uint8Array}> = execFile("git", [], widenedOptions);
const widenedShellOutput: Promise<{stdout: string | Uint8Array; stderr: string | Uint8Array}> = exec("printf test", widenedOptions);
const widenedSyncOutput: {stdout: string | Uint8Array; stderr: string | Uint8Array; status: number; signal: string | null; error?: Error} = spawnSync("git", [], widenedOptions);
const generalExecOptions: import("node:child_process").ExecOptions = {encoding: "buffer"};
const generalEncodingOutput: Promise<{stdout: string | Uint8Array; stderr: string | Uint8Array}> = execFile("git", [], generalExecOptions);
const optionalBufferOptions: {encoding?: "buffer"} = {};
const optionalBufferOutput: Promise<{stdout: string | Uint8Array; stderr: string | Uint8Array}> = exec("printf test", optionalBufferOptions);
const definiteBufferOptions = {encoding: "buffer"} as const;
const definiteBufferedOutput: Promise<{stdout: Uint8Array; stderr: Uint8Array}> = execFile("git", [], definiteBufferOptions);
const knownTextOptions = {encoding: "utf8"} as const;
const definiteTextOutput: Promise<{stdout: string; stderr: string}> = execFile("git", [], knownTextOptions);
const syncResult = spawnSync("git", ["status"], {encoding: "buffer"});
const binaryOutput: Uint8Array = syncResult.stdout;
const plainResult = spawnSync("git", ["status"]);
const plainOutput: string = plainResult.stdout;
const v4Records: Promise<string[]> = dnsPromises.resolve4("example.test");
resolve4Callback("example.test", (_error, records) => {
  const firstRecord: string | undefined = records?.[0];
  void firstRecord;
});
const promiseResolver = new dnsPromises.Resolver();
const resolverServers: string[] = promiseResolver.getServers();
lookupCallback("", (_error, address) => {
  if (address === null) {
    const noAddress: null = address;
    void noAddress;
  }
});
const fileText: string = readFileSync("README.md", "utf8");
const fileBytes: Uint8Array = readFileSync("README.md");
const fileSize: number = statSync("README.md").size;
const asyncText: Promise<string> = readFile("README.md", "utf8");
const names: Promise<string[]> = readdir(".");
const server = nodeServe({port: 0}, request => Response.json({path: new URL(request.url).pathname}));
server.close();
const hostPlatform: string = platform();
const cpuCount: number = cpus();
const resolvedPath: string = join("docs", "README.md");
const winPath: string = path.win32.join("C:\\", "tmp");
const posixPath: string = posix.join("/tmp", "file");
const currentDirectory: string = processModule.cwd();
const query = parseQuery("tag=a&tag=b");
const tagValues: string | string[] = query.tag;
const bytesStream = new ModuleReadableStream<Uint8Array>({
  pull(controller) {
    controller.enqueue(new Uint8Array([1, 2]));
    controller.close();
  },
});
const decoder = new StringDecoder("utf8");
const decoded: string = decoder.write(bytes);
const timerResult: Promise<number> = delay(1, 42);
const fileURL: URL = pathToFileURL("README.md");
const asyncFunction = promisify((value: string, callback: (error: Error | null, result: number) => void) => callback(null, value.length));
const lengthPromise: Promise<number> = asyncFunction("NOE");
const vmValue: unknown = runInNewContext("answer", {answer: 42});
const worker = new NodeWorker("./worker.mjs", {name: "fixture", env: WORKER_SHARE_ENV});
const exitCode: Promise<number | undefined> = worker.terminate();
const zipped: Uint8Array = gzipSync(bytes);

const docsText: string = docs("noe:fs-handle", "open");
const inputStream: ReadableStream<Uint8Array> = stdin;
const outputStream: WritableStream<Uint8Array> = stdout;
const pipeResult: Promise<void> = pipe(inputStream, outputStream, {preventClose: true});
const ownedChild = await spawnOwned("echo", ["hello"], {stdout: "pipe"});
const ownedStatus: number | null = (await ownedChild.exited).code;
await ownedChild.close();
const fetched: Promise<Response> = noeFetch("https://example.test");
const noeServer = noeServe(request => Response.json({url: request.url}));
noeServer.close();
const fileHandle = await open("README.md", {mode: "r"});
const fileRead = await fileHandle.readAt(0, 8);
const readCount: number = fileRead.bytesRead;
await fileHandle.close();
const socketConnect = connect({network: "tcp", host: "127.0.0.1", port: 9000});
const socketListener = listen({network: "unix", path: "/tmp/noe.sock"});
const signalIterator = subscribe("SIGTERM");
await signalIterator.close();

const controller = new AbortController();
const request = new Request("https://example.test", {signal: controller.signal});
const response: Promise<Response> = fetch(request);
const parsedBody: Promise<unknown> = request.json();
const stream = ReadableStream.from([new Uint8Array([3])]);
const streamReader = stream.getReader();
const streamChunk: Promise<ReadableStreamReadResult<Uint8Array>> = streamReader.read();
const encoded: Uint8Array = new TextEncoder().encode("hello");
const text: string = new TextDecoder().decode(encoded);
const webSocket = new WebSocket("wss://example.test");
webSocket.addEventListener("message", event => console.log(event.data));
const messageChannel = new MessageChannel();
if (!(messageChannel.port1 instanceof MessagePort)) throw new Error("MessagePort instance check");
messageChannel.port1.postMessage({kind: "fixture"});
const digest: Promise<ArrayBuffer> = crypto.subtle.digest("SHA-256", encoded);
const compressed = new CompressionStream("gzip");
const cloned = structuredClone({bytes: encoded}, {transfer: []});
const timerId: number = setTimeout(() => console.log(cloned.bytes.length), 1);
clearTimeout(timerId);
const currentPid: number = process.pid;

void [assertAlias, asyncHooksAlias, bufferAlias, childProcessAlias, cryptoAlias, dnsAlias, dnsPromisesAlias,
  eventsAlias, fsAlias, fsPromisesAlias, httpAlias, osAlias, pathAlias, pathPosixAlias, pathWin32Alias,
  processAlias, querystringAlias, streamWebAlias, stringDecoderAlias, timersAlias, timersPromisesAlias,
  urlAlias, utilAlias, vmAlias, workerAlias, zlibAlias, bufferValue, hash, random, command, binaryOutput,
  fileText, fileBytes, fileSize, asyncText, names, hostPlatform, cpuCount, resolvedPath, winPath, posixPath,
  currentDirectory, tagValues, decoded, timerResult, fileURL, lengthPromise, vmValue, exitCode, zipped, plainOutput, v4Records, resolverServers, binaryCommand, binaryShell,
  widenedFileOutput, widenedShellOutput, widenedSyncOutput, generalEncodingOutput, optionalBufferOutput, definiteBufferedOutput, definiteTextOutput,
  docsText, ownedStatus, fetched, readCount, socketConnect, socketListener, response, parsedBody, streamChunk,
  digest, compressed, currentPid];
