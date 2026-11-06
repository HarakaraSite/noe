import assert, {deepStrictEqual, rejects as assertRejects, strict} from "node:assert";
import {AsyncLocalStorage} from "async_hooks";
import bufferDefault, {Buffer} from "node:buffer";
import childProcessDefault, {exec, execFile, spawn as spawnBare, spawnSync, type ChildProcess, type SpawnOptions} from "child_process";
import nodeChildProcessDefault, {spawn as spawnOwned, type ChildProcess as CanonicalChildProcess, type SpawnOptions as CanonicalSpawnOptions} from "node:child_process";
import {createHash, randomBytes} from "crypto";
import dns, {lookup as lookupCallback, resolve4 as resolve4Callback} from "node:dns";
import * as dnsPromises from "dns/promises";
import {EventEmitter, once} from "events";
import fs, {promises as fsPromises, readFileSync, statSync} from "node:fs";
import {watch as watchFiles} from "node:fs";
import {watch as watchFileEvents, WatchEvent, WatchIterator} from "node:fs/promises";
import fsPromisesDefault, {
  FileHandleRead,
  FileHandleReadOptions,
  FileHandleWrite,
  FileSystemError,
  open as nodeFsOpen,
} from "node:fs/promises";
import {FileHandle, open as bareFsOpen, readFile, readdir} from "fs/promises";
import bareFsPromisesDefault from "fs/promises";
import {serve as nodeServe} from "node:http";
import {platform, cpus} from "os";
import path, {join, posix} from "node:path";
import processModule, {stdin as processInput, stdout as processOutput, stderr as processError, type ProcessStdio} from "node:process";
import processBare, {stdin as bareProcessInput, stdout as bareProcessOutput, stderr as bareProcessError} from "process";
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
import {fetch as noeFetch, serve as noeServe} from "noe:http";
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
const sameProcessDefault: typeof processModule = processBare;
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

const docsText: string = docs("node:fs/promises", "open");
const standardStreams: ProcessStdio = {stdin: processInput, stdout: processOutput, stderr: processError};
const inputStream: ReadableStream<Uint8Array> = processInput;
const outputStream: WritableStream<Uint8Array> = processOutput;
const errorStream: WritableStream<Uint8Array> = processError;
const pipeResult: Promise<void> = inputStream.pipeTo(outputStream, {preventClose: true});
const sameDefaultInput: ReadableStream<Uint8Array> = processModule.stdin!;
const sameDefaultOutput: WritableStream<Uint8Array> = processModule.stdout!;
const sameDefaultError: WritableStream<Uint8Array> = processModule.stderr!;
const bareInputStream: ReadableStream<Uint8Array> = bareProcessInput;
const bareOutputStream: WritableStream<Uint8Array> = bareProcessOutput;
const bareErrorStream: WritableStream<Uint8Array> = bareProcessError;
const bareProcessInputMatches: typeof processInput = bareProcessInput;
const bareProcessOutputMatches: typeof processOutput = bareProcessOutput;
const bareProcessErrorMatches: typeof processError = bareProcessError;
const globalProcessOutput: WritableStream<Uint8Array> = process.stdout!;
const ownedOptions: CanonicalSpawnOptions = {stdout: "pipe", env: {MODE: "worker", OMIT_THIS: null}};
const ownedChild: CanonicalChildProcess = await spawnOwned("echo", ["hello"], ownedOptions);
const bareOwnedChild: ChildProcess = await spawnBare("echo", ["hello"], {stdout: "pipe"});
const defaultSpawn: typeof spawnOwned = childProcessDefault.spawn;
const canonicalDefaultSpawn: typeof spawnOwned = nodeChildProcessDefault.spawn;
const bareChildTypeMatches: ChildProcess = ownedChild;
const canonicalOptions: SpawnOptions = ownedOptions;
const ownedStatus: number | null = (await ownedChild.exited).code;
await ownedChild.close();
await bareOwnedChild.close();
const fetched: Promise<Response> = noeFetch("https://example.test");
const noeServer = noeServe(request => Response.json({url: request.url}));
noeServer.close();
const fileHandle: FileHandle = await nodeFsOpen("README.md");
const bareFsPromiseOpen: typeof nodeFsOpen = bareFsOpen;
const namedNodeFsPromiseOpen: typeof nodeFsOpen = fs.promises.open;
const namedNodeFsPromisesImportOpen: typeof nodeFsOpen = fsPromises.open;
const defaultNodeFsPromiseOpen: typeof nodeFsOpen = fsPromisesDefault.open;
const defaultBareFsPromiseOpen: typeof nodeFsOpen = bareFsPromisesDefault.open;
const readBuffer = new Uint8Array(8);
const fileRead = await fileHandle.read(readBuffer, 0, readBuffer.byteLength, 0);
const sameReadBuffer: Uint8Array = fileRead.buffer;
const optionRead = await fileHandle.read(readBuffer, {offset: 1, length: 2, position: 1});
const sameOptionReadBuffer: Uint8Array = optionRead.buffer;
const defaultRead = await fileHandle.read();
const defaultReadBuffer: Uint8Array = defaultRead.buffer;
const dataView = new DataView(new ArrayBuffer(4));
const viewOptions: FileHandleReadOptions<DataView> = {buffer: dataView, offset: 0, length: 4, position: 0};
const viewRead = await fileHandle.read(viewOptions);
const typedViewRead: FileHandleRead<DataView> = viewRead;
const sameDataView: DataView = viewRead.buffer;
const readCount: number = fileRead.bytesRead;
const byteWrite = await fileHandle.write(readBuffer, {offset: 0, length: 2, position: 0});
const sameWriteBuffer: Uint8Array = byteWrite.buffer;
const positionalWrite = await fileHandle.write(readBuffer, 0, 2, 0);
const samePositionalWriteBuffer: Uint8Array = positionalWrite.buffer;
const textWrite = await fileHandle.write("text", 0, "utf-8");
const typedTextWrite: FileHandleWrite<string> = textWrite;
const sameText: string = textWrite.buffer;
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
const transformed = new TransformStream<Uint8Array, Uint8Array>({
  transform(chunk, controller) { controller.enqueue(chunk); },
  cancel(reason: unknown) { console.log(reason); },
});
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
  docsText, ownedStatus, fetched, readCount, sameReadBuffer, sameOptionReadBuffer, defaultReadBuffer, sameDataView,
  defaultSpawn, canonicalDefaultSpawn, bareChildTypeMatches, canonicalOptions,
  typedViewRead, sameWriteBuffer, samePositionalWriteBuffer, typedTextWrite, sameText,
  bareFsPromiseOpen, namedNodeFsPromiseOpen, namedNodeFsPromisesImportOpen,
  defaultNodeFsPromiseOpen, defaultBareFsPromiseOpen,
  socketConnect, socketListener, response, parsedBody, streamChunk,
  digest, compressed, transformed, currentPid];

// FileHandle metadata, resizing, and durable flush are promise operations.
const openCreated = await nodeFsOpen("created.bin", "wx+", 0o600);
const handleInfo: NOEStdlib.FileInfo = await openCreated.stat();
const handleMtime: Date = handleInfo.mtime;
const handleMtimeMs: number = handleInfo.mtimeMs;
const handleMode: number = handleInfo.mode;
const fileKindBits: number = handleMode & 0o170000;
const filePermissionBits: number = handleMode & 0o777;
const filesystemError: FileSystemError = Object.assign(new Error("missing"), {
  code: "ENOENT",
  errno: 2,
  path: "missing",
  syscall: "open",
});
await openCreated.truncate(4);
await openCreated.sync();
await openCreated.close();
const syncDirents = fs.readdirSync(".", {withFileTypes: true});
const asyncDirents = await readdir(".", {withFileTypes: true});
const entryNames: string[] = syncDirents.map(entry => entry.name);
const entryParent: string | undefined = asyncDirents[0]?.parentPath;
const isEntryFile: boolean | undefined = asyncDirents[0]?.isFile();
const variableDirents: string[] | NOEStdlib.Dirent[] = fs.readdirSync(".", {withFileTypes: Math.random() > 0.5});
void [handleInfo, handleMtime, handleMtimeMs, fileKindBits, filePermissionBits, filesystemError,
  entryNames, entryParent, isEntryFile, variableDirents];

// Combined cancellation and the engine's explicit resource-management syntax.
const combinedSignal: AbortSignal = AbortSignal.any(new Set([new AbortController().signal, AbortSignal.abort("stop")]));
async function managedResources(): Promise<void> {
  const {open} = await import("node:fs/promises");
  const {spawn} = await import("node:child_process");
  const {listen, connect} = await import("noe:sockets");
  const {subscribe} = await import("noe:signals");
  await using file = await open("value");
  await using child = await spawn("tool");
  await using listener = await listen({network: "tcp", host: "127.0.0.1", port: 0});
  await using connection = await connect({network: "tcp", host: "127.0.0.1", port: 1});
  await using subscription = subscribe("SIGINT");
  const disposable: AsyncDisposable = file;
  const disposed: Promise<void> = child[Symbol.asyncDispose]();
  void [listener, connection, subscription, disposable, disposed];
}
void [combinedSignal, managedResources];

// Watch callbacks and iterators retain their selected filename representation.
const watchedText = watchFiles(".", (_type, filename) => { const text: string | null = filename; void text; });
const watchedBytes = watchFiles(".", {encoding: "buffer", persistent: false}, (_type, filename) => {
  const bytes: import("node:buffer").Buffer | null = filename; void bytes;
});
watchedText.on("change", (_type, filename) => { const text: string | null = filename; void text; });
watchedBytes.unref().ref().close();
const watchedStringEncoding = fs.watch(".", "utf-8");
const watchedIterator: WatchIterator = watchFileEvents(".", {signal: AbortSignal.any([]), maxQueue: 32, overflow: "error"});
const watchedBufferIterator: WatchIterator<import("node:buffer").Buffer> = fsPromises.watch(".", "buffer");
for await (const event of watchedIterator) { const typed: WatchEvent = event; void typed; break; }
await watchedBufferIterator.return();
await watchedIterator.close();
{
  await using scopedWatcher = watchedText;
  await using scopedEvents = watchFileEvents(".", {recursive: false});
  void [scopedWatcher, scopedEvents];
}
void watchedStringEncoding;

// NOE adds optional handshake headers without changing the protocols slot.
const websocketOptions: WebSocketOptions = {headers: {Authorization: "Bearer fixture"}};
new WebSocket("wss://example.test", ["chat.v1"], websocketOptions);
new WebSocket("wss://example.test", undefined, {headers: new Headers({Origin: "https://app.test"})});
new WebSocket("wss://example.test", "chat.v1", {headers: [["X-Test", "value"]]});
new WebSocket("wss://example.test", undefined, {headers: new Map<string, string>([["X-Test", "value"]])});
new WebSocket("wss://example.test", undefined, {headers: null});
