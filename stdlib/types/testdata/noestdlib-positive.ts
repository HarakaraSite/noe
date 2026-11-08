import assert, {deepStrictEqual, rejects as assertRejects, strict} from "node:assert";
import {AsyncLocalStorage} from "async_hooks";
import bufferDefault, {Buffer} from "node:buffer";
import processAPI, {
  exec,
  execFile,
  execFileSync,
  execSync,
  process as processInfo,
  spawn as spawnBare,
  spawnSync,
  stdin as processInput,
  stdout as processOutput,
  stderr as processError,
  type ChildProcess,
  type Process,
  type ProcessStdio,
  type SpawnOptions,
} from "noe:process";
import {createHash, randomBytes} from "crypto";
import dns, {lookup as lookupCallback, resolve4 as resolve4Callback} from "node:dns";
import * as dnsPromises from "dns/promises";
import {EventEmitter, once} from "events";
import fs, * as noeFs from "noe:fs";
import {
  appendFile,
  chmod,
  open as openFile,
  readFile,
  readdir,
  watch,
  type FileHandle,
  type FileHandleFlags,
  type FileHandleOpenOptions,
  type FileSystemError,
  type WatchEvent,
  type WatchIterator,
} from "noe:fs";
import {platform, cpus} from "os";
import path, {join, posix} from "node:path";
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
import {
  createWebSocket,
  serve as noeServe,
  upgradeWebSocket,
  type CreateWebSocketOptions,
  type FetchHandler,
  type HTTPServer,
  type HTTPUpgradeOptions,
  type ServeOptions,
  type WebSocketUpgrade,
} from "noe:http";
import {bind, connect, listen, type Address, type BindOptions, type ConnectOptions, type Datagram, type DatagramAddress, type DatagramOperationOptions, type DatagramSocket, type ListenOptions} from "noe:sockets";
import {subscribe} from "noe:signals";
import {open as openSQLite, type SQLiteDatabase, type SQLiteOutputValue, type SQLiteParams, type SQLiteRow} from "noe:sqlite";

// Every registered bare name resolves to the same declared surface as its
// node:-prefixed canonical name.
const assertAlias: typeof import("assert") = undefined as unknown as typeof import("node:assert");
const asyncHooksAlias: typeof import("async_hooks") = undefined as unknown as typeof import("node:async_hooks");
const bufferAlias: typeof import("buffer") = undefined as unknown as typeof import("node:buffer");
const cryptoAlias: typeof import("crypto") = undefined as unknown as typeof import("node:crypto");
const dnsAlias: typeof import("dns") = undefined as unknown as typeof import("node:dns");
const dnsPromisesAlias: typeof import("dns/promises") = undefined as unknown as typeof import("node:dns/promises");
const eventsAlias: typeof import("events") = undefined as unknown as typeof import("node:events");
const osAlias: typeof import("os") = undefined as unknown as typeof import("node:os");
const pathAlias: typeof import("path") = undefined as unknown as typeof import("node:path");
const pathPosixAlias: typeof import("path/posix") = undefined as unknown as typeof import("node:path/posix");
const pathWin32Alias: typeof import("path/win32") = undefined as unknown as typeof import("node:path/win32");
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
const generalExecOptions: import("noe:process").ExecOptions = {encoding: "buffer"};
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
const fileText: string = fs.readFileSync("README.md", "utf8");
const fileBytes: Uint8Array = fs.readFileSync("README.md");
const fileSize: number = fs.statSync("README.md").size;
const asyncText: Promise<string> = readFile("README.md", "utf8");
const names: Promise<string[]> = readdir(".");
const hostPlatform: string = platform();
const cpuCount: number = cpus();
const resolvedPath: string = join("docs", "README.md");
const winPath: string = path.win32.join("C:\\", "tmp");
const posixPath: string = posix.join("/tmp", "file");
const currentDirectory: string = processInfo.cwd();
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

const docsText: string = docs("noe:fs", "open");
const standardStreams: ProcessStdio = {stdin: processInput, stdout: processOutput, stderr: processError};
const inputStream: ReadableStream<Uint8Array> = processInput;
const outputStream: WritableStream<Uint8Array> = processOutput;
const errorStream: WritableStream<Uint8Array> = processError;
const pipeResult: Promise<void> = inputStream.pipeTo(outputStream, {preventClose: true});
const namespaceInput: ReadableStream<Uint8Array> = processAPI.stdin;
const namespaceOutput: WritableStream<Uint8Array> = processAPI.stdout;
const namespaceError: WritableStream<Uint8Array> = processAPI.stderr;
const processInputProperty: ReadableStream<Uint8Array> = processInfo.stdin!;
const processOutputProperty: WritableStream<Uint8Array> = processInfo.stdout!;
const processErrorProperty: WritableStream<Uint8Array> = processInfo.stderr!;
const globalProcessOutput: WritableStream<Uint8Array> = process.stdout!;
const namedProcessMatchesGlobal: typeof process = processInfo;
const typedProcessInfo: Process = processInfo;
const defaultNamespaceProcessMatchesNamed: typeof processInfo = processAPI.process;
const ownedOptions: SpawnOptions = {stdout: "pipe", env: {MODE: "worker", OMIT_THIS: null}};
const ownedChild: ChildProcess = await spawnBare("echo", ["hello"], ownedOptions);
const bareOwnedChild: ChildProcess = await spawnBare("echo", ["hello"], {stdout: "pipe"});
const defaultSpawn: typeof spawnBare = processAPI.spawn;
const defaultCommandSync: typeof spawnSync = processAPI.spawnSync;
const synchronousFileCommand: string = execFileSync("git", ["status", "--short"]);
const synchronousShellCommand: string = execSync("git status --short");
const bareChildTypeMatches: ChildProcess = ownedChild;
const ownedStatus: number | null = (await ownedChild.exited).code;
await ownedChild.close();
await bareOwnedChild.close();
const fetched: Promise<Response> = fetch("https://example.test");
const eventSource = new EventSource(new URL("https://example.test/events"), {withCredentials: true});
eventSource.onmessage = event => { const data: string = event.data; const id: string = event.lastEventId; void [data, id]; };
eventSource.addEventListener("message", event => { const data: string = event.data; void data; });
eventSource.addEventListener("update", event => { const type: string = event.type; void type; });
const eventSourceOpen: 1 = eventSource.OPEN;
const eventSourceClosed: 2 = EventSource.CLOSED;
eventSource.close();
async function sqliteTypeFixture(): Promise<void> {
  await using database = await openSQLite();
  const typedDatabase: SQLiteDatabase = database;
  const controller = new AbortController();
  const blob = new Uint8Array([1, 2, 3]);
  const positional: SQLiteParams = [null, undefined, true, 1, 2n, "value", blob.buffer, new DataView(new ArrayBuffer(4))];
  const named: SQLiteParams = {id: 2n, value: blob, shared: new SharedArrayBuffer(4)};
  await database.exec("CREATE TABLE sample (id INTEGER, value BLOB)", {signal: controller.signal});
  const result = await database.run("INSERT INTO sample VALUES (?, ?)", [2n, blob], {signal: controller.signal});
  const changes: number | bigint = result.changes;
  const insertedId: number | bigint = result.lastInsertRowid;
  const row: SQLiteRow | undefined = await database.get("SELECT id, value FROM sample WHERE id = ?", [2n]);
  const value: SQLiteOutputValue | undefined = row?.value;
  const rows: SQLiteRow[] = await database.all("SELECT id, value FROM sample", named);
  const statement = await database.prepare("SELECT id, value FROM sample WHERE id = ?");
  const statementRow: SQLiteRow | undefined = await statement.get(positional);
  await statement.close();
  const committed: bigint = await database.transaction(async tx => {
    await tx.run("INSERT INTO sample VALUES (?, ?)", {id: 3n, value: blob});
    return 3n;
  }, {mode: "immediate", signal: controller.signal});
  void [typedDatabase, changes, insertedId, value, rows, statementRow, committed];
}
void sqliteTypeFixture;
const noeServer = noeServe(request => Response.json({url: request.url}));
const noeServerClose: Promise<void> = noeServer.close();
const repeatedNoeServerClose: Promise<void> = noeServer.close();
const typedServer: HTTPServer = noeServe(request => Response.json({url: request.url}));
const noeServerAsyncDispose: Promise<void> = typedServer[Symbol.asyncDispose]();
async function asyncDisposableHTTPServer(): Promise<void> {
  await using server = noeServe(request => Response.json({url: request.url}));
  void server.url;
}
const serveOptions: ServeOptions = {hostname: "127.0.0.1", port: 0};
const handler: FetchHandler = request => Response.json({url: request.url});
const certificateText: string = await readFile("server-cert.pem", "utf8");
const privateKeyText: string = await readFile("server-key.pem", "utf8");
const httpsServeOptions: ServeOptions = {
  hostname: "127.0.0.1",
  port: 8443,
  cert: certificateText,
  key: privateKeyText,
};
const httpsServer: HTTPServer = noeServe(httpsServeOptions, handler);
const httpsServerURL: string = httpsServer.url;
const upgradeOptions: HTTPUpgradeOptions = {protocol: "chat.v1"};
const upgrade: WebSocketUpgrade = upgradeWebSocket(new Request("http://example.test/ws"), upgradeOptions);
const upgradedSocket: WebSocket = upgrade.socket;
const createOptions: CreateWebSocketOptions = {
  protocols: ["chat.v1"],
  headers: new Headers({Authorization: "Bearer fixture"}),
};
const customSocket: WebSocket = createWebSocket("wss://example.test", createOptions);
const directSocket: WebSocket = createWebSocket("wss://example.test", {
  protocols: "chat.v1",
  headers: [["X-Test", "value"]],
});
const fileHandle: FileHandle = await openFile("README.md");
const namedModuleOpen: typeof openFile = noeFs.open;
const defaultModuleOpen: typeof openFile = fs.open;
const fileHandleFlags: FileHandleFlags = "r+";
const fileHandleOpenOptions: FileHandleOpenOptions = {flags: fileHandleFlags, mode: 0o600};
const readBuffer = new Uint8Array(8);
const fileRead: Uint8Array = await fileHandle.read(readBuffer.byteLength);
const positionalRead: Uint8Array = await fileHandle.readAt(1, 2);
const readAtEOF: Uint8Array = await fileHandle.readAt(0, 0);
const byteWrite: number = await fileHandle.write(readBuffer);
const positionalWrite: number = await fileHandle.writeAt(0, readBuffer.subarray(0, 2));
const textWrite: number = await fileHandle.write("text");
await fileHandle.close();
const socketConnect = connect({network: "tcp", host: "127.0.0.1", port: 9000});
const socketListener = listen({network: "unix", path: "/tmp/noe.sock"});
const tlsConnectOptions: ConnectOptions = {
  network: "tls",
  host: "localhost",
  port: 9000,
  serverName: "localhost",
  ca: "-----BEGIN CERTIFICATE-----\\n...\\n-----END CERTIFICATE-----\\n",
  cert: "-----BEGIN CERTIFICATE-----\\n...\\n-----END CERTIFICATE-----\\n",
  key: "-----BEGIN PRIVATE KEY-----\\n...\\n-----END PRIVATE KEY-----\\n",
};
const tlsListenOptions: ListenOptions = {
  network: "tls",
  host: "127.0.0.1",
  port: 0,
  cert: "-----BEGIN CERTIFICATE-----\\n...\\n-----END CERTIFICATE-----\\n",
  key: "-----BEGIN PRIVATE KEY-----\\n...\\n-----END PRIVATE KEY-----\\n",
};
const tlsAddress: Address = {network: "tls", host: "127.0.0.1", port: 9000};
const tlsConnect = connect(tlsConnectOptions);
const tlsListener = listen(tlsListenOptions);
void [socketConnect, socketListener, tlsConnect, tlsListener, tlsAddress];
const udpBindOptions: BindOptions = {network: "udp", host: "127.0.0.1", port: 0};
const udpBind = bind(udpBindOptions);
const udpAddress: DatagramAddress = {network: "udp", host: "127.0.0.1", port: 53};
const udpOptions: DatagramOperationOptions = {signal: new AbortController().signal};
const udpSocket: Promise<DatagramSocket> = udpBind;
const udpOperation = async (socket: DatagramSocket): Promise<Datagram> => {
  await socket.send(new Uint8Array(), udpAddress, udpOptions);
  return socket.receive(udpOptions);
};
void [udpSocket, udpOperation];
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
const customUpdate: CustomEvent<{value: number}> = new CustomEvent("update", {
  bubbles: true, composed: true, cancelable: true, detail: {value: 42},
});
const customValue: number = customUpdate.detail.value;
customUpdate.initCustomEvent("updated", false, false, {value: customValue});
const userMark: PerformanceMark = performance.mark("start", {startTime: 1, detail: {value: 1}});
const userMeasure: PerformanceMeasure = performance.measure("work", {start: "start", end: 3});
const legacyMeasure: PerformanceMeasure = performance.measure("legacy", "start", "end");
const emptyOptionsMeasure: PerformanceMeasure = performance.measure("null-options", null);
const standaloneMark: PerformanceMark = new PerformanceMark("standalone", {startTime: 2});
const timeline: PerformanceEntry[] = performance.getEntriesByName("work", "measure");
const markDetail: unknown = userMark.toJSON().detail;
performance.clearMarks("start"); performance.clearMeasures();
const text: string = new TextDecoder().decode(encoded);
const sharedText: string = new TextDecoder(" UTF-8 ", null).decode(new SharedArrayBuffer(0), null);
const decoderStreamWrite: Promise<void> = new TextDecoderStream().writable.getWriter().write(new ArrayBuffer(0));
const fatalDecoderStream = new TextDecoderStream("utf-8", {fatal: true, ignoreBOM: true});
const fatalStreamOption: boolean = fatalDecoderStream.fatal;
const ignoreBOMStreamOption: boolean = fatalDecoderStream.ignoreBOM;
const decoderReadable: ReadableStream<string> = fatalDecoderStream.readable;
const encoderWritable: WritableStream<string> = new TextEncoderStream().writable;
const signalInstance: boolean = controller.signal instanceof AbortSignal;
const routeInit: URLPatternInit = {pathname: "books/:id", baseURL: "https://example.test/api/"};
const routeOptions: URLPatternOptions = {ignoreCase: true};
const routePattern = new URLPattern(routeInit, routeOptions);
const routeFound: URLPatternResult | null = routePattern.exec({pathname:"/api/books/1",baseURL:"https://example.test"});
const routeInputMatch: boolean = routePattern.test(new URL("https://example.test/api/books/1"));
const emptyPatternResult: URLPatternResult | null = new URLPattern().exec();
const patternHasRegex: boolean = routePattern.hasRegExpGroups;
const queryInit: URLSearchParamsInit = new Set<readonly [string, string]>([["a", "one"]]);
const queryParams = new URLSearchParams(queryInit);
const querySize: number = queryParams.size;
const queryHasValue: boolean = queryParams.has("a", "one");
const requestPrototype: Request = Request.prototype;
const extensionMethod: string = new Request("https://example.test/", {method:"patch"}).method;
queryParams.delete("a", "one");
const compressionViewWrite: Promise<void> = new CompressionStream("gzip").writable.getWriter().write(new DataView(new ArrayBuffer(2)));
const listenerTarget = new EventTarget();
const listenerOptions: AddEventListenerOptions = {capture: true, once: true, passive: true, signal: controller.signal};
const captureOptions: EventListenerOptions = {capture: true};
const listener: EventListener = event => { void event.type; };
listenerTarget.addEventListener("change", listener, listenerOptions);
listenerTarget.removeEventListener("change", listener, captureOptions);
listenerTarget.addEventListener("change", {handleEvent(event) { void event.type; }}, true);
listenerTarget.removeEventListener("change", null, null);
const webSocket = new WebSocket("wss://example.test");
webSocket.addEventListener("message", event => console.log(event.data), listenerOptions);
webSocket.removeEventListener("message", event => console.log(event.data), true);
const messageChannel = new MessageChannel();
if (!(messageChannel.port1 instanceof MessagePort)) throw new Error("MessagePort instance check");
messageChannel.port1.postMessage({kind: "fixture"});
const digest: Promise<ArrayBuffer> = crypto.subtle.digest("SHA-256", encoded);
const webKey = await crypto.subtle.importKey("raw", new DataView(new ArrayBuffer(16)),
  {name: "HMAC", hash: "SHA-256", length: 128}, false,
  new Set<NOEStdlib.CryptoKeyUsage>(["verify"]));
const hmacBits: number | undefined = webKey.algorithm.length;
const verifiedMAC: Promise<boolean> = crypto.subtle.verify("hmac", webKey, encoded, encoded);
// Cached metadata objects are mutable views, not the internal key policy.
webKey.algorithm.name = "visible metadata";
webKey.usages.push("sign");
const derivedWebKey: Promise<NOEStdlib.CryptoKey> = crypto.subtle.deriveKey(
  {name: "HKDF", hash: "SHA-256", salt: encoded, info: encoded}, webKey,
  {name: "HMAC", hash: "SHA-256"}, false, ["sign"]);
void [hmacBits, verifiedMAC, derivedWebKey];
const compressed = new CompressionStream("gzip");
const transformed = new TransformStream<Uint8Array, Uint8Array>({
  transform(chunk, controller) { controller.enqueue(chunk); },
  cancel(reason: unknown) { console.log(reason); },
});
const cloned = structuredClone({bytes: encoded}, {transfer: []});
const timerId: number = setTimeout(() => console.log(cloned.bytes.length), 1);
clearTimeout(timerId);
const currentPid: number = process.pid;

void [assertAlias, asyncHooksAlias, bufferAlias, cryptoAlias, dnsAlias, dnsPromisesAlias,
  eventsAlias, osAlias, pathAlias, pathPosixAlias, pathWin32Alias,
  querystringAlias, streamWebAlias, stringDecoderAlias, timersAlias, timersPromisesAlias,
  urlAlias, utilAlias, vmAlias, workerAlias, zlibAlias, bufferValue, hash, random, command, binaryOutput,
  fileText, fileBytes, fileSize, asyncText, names, hostPlatform, cpuCount, resolvedPath, winPath, posixPath,
  currentDirectory, tagValues, decoded, timerResult, fileURL, lengthPromise, vmValue, exitCode, zipped, plainOutput, v4Records, resolverServers, binaryCommand, binaryShell,
  widenedFileOutput, widenedShellOutput, widenedSyncOutput, generalEncodingOutput, optionalBufferOutput, definiteBufferedOutput, definiteTextOutput,
  docsText, ownedStatus, fetched, noeServer, noeServerClose, repeatedNoeServerClose,
  typedServer, noeServerAsyncDispose, asyncDisposableHTTPServer, upgrade, upgradedSocket,
  customSocket, directSocket,
  serveOptions, httpsServer, httpsServerURL, handler,
  namespaceInput, namespaceOutput, namespaceError, processInputProperty, processOutputProperty,
  processErrorProperty, namedProcessMatchesGlobal, typedProcessInfo, defaultNamespaceProcessMatchesNamed,
  defaultSpawn, defaultCommandSync, synchronousFileCommand, synchronousShellCommand,
  bareChildTypeMatches,
  namedModuleOpen, defaultModuleOpen, fileHandleOpenOptions,
  fileRead, positionalRead, readAtEOF, byteWrite, positionalWrite, textWrite,
  socketConnect, socketListener, response, parsedBody, streamChunk,
  digest, compressed, transformed, currentPid];

// FileHandle metadata, resizing, and durable flush are promise operations.
const openCreated = await openFile("created.bin", {flags: "wx+", mode: 0o600});
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
await appendFile("notes.txt", "more\n");
await chmod("notes.txt", 0o600);

// Combined cancellation and the engine's explicit resource-management syntax.
const combinedSignal: AbortSignal = AbortSignal.any(new Set([new AbortController().signal, AbortSignal.abort("stop")]));
async function managedResources(): Promise<void> {
  const {open} = await import("noe:fs");
  const {spawn} = await import("noe:process");
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

// Watch iterators retain their selected filename representation.
const watchedIterator: WatchIterator = watch(".", {signal: AbortSignal.any([]), maxQueue: 32, overflow: "error"});
const watchedBufferIterator: WatchIterator<import("node:buffer").Buffer> = watch(".", "buffer");
for await (const event of watchedIterator) { const typed: WatchEvent = event; void typed; break; }
await watchedBufferIterator.return();
await watchedIterator.close();
{
  await using scopedEvents = watch(".", {recursive: false});
  void scopedEvents;
}
void watchedBufferIterator;

// The global constructor follows the standard two-argument API; use
// createWebSocket for custom handshake headers.
const standardSocket = new WebSocket("wss://example.test", ["chat.v1"]);
const standardSocketURL = new WebSocket(new URL("wss://example.test"));
void [standardSocket, standardSocketURL];

const storage: Storage = sessionStorage;
const storageLength: number = storage.length;
const storageItem: string | null = storage.getItem("example");
const storageKey: string | null = storage.key(0);
storage.setItem("example", "value");
storage.removeItem("example");
storage.clear();
storage["named"] = "value";
delete storage["named"];
const persistentStorage: Storage = localStorage;
void [storageLength, storageItem, storageKey, persistentStorage];
