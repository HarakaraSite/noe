/**
 * Ambient declarations for the NOE-managed JavaScript standard library.
 *
 * These declarations describe the exported surface implemented by NOE; they
 * do not imply that a given Profile installed the capability needed by a
 * module or global. In particular, filesystem, process, network, timers,
 * sockets, signals, and worker APIs depend on runtime configuration. Worker
 * globals also differ from the main runtime. The Web declarations below cover
 * the subset NOE installs and intentionally do not stand in for the DOM lib.
 */

declare namespace NOEStdlib {
  type ByteInput = string | ArrayBuffer | ArrayBufferView;
  type BufferEncoding = "ascii" | "base64" | "binary" | "hex" | "latin1" | "utf-8" | "utf8";
  type IntegerTypedArray = Int8Array | Uint8Array | Uint8ClampedArray | Int16Array | Uint16Array | Int32Array | Uint32Array | BigInt64Array | BigUint64Array;
  type CompressionFormat = "deflate" | "deflate-raw" | "gzip";
  type SignalName = "SIGHUP" | "SIGINT" | "SIGTERM" | "SIGUSR1" | "SIGUSR2";
  type DNSResultOrder = "ipv4first" | "ipv6first" | "verbatim";
  type DNSFamily = 0 | 4 | 6 | "IPv4" | "IPv6";
  type DNSRecordType = "A" | "AAAA" | "ANY" | "CAA" | "CNAME" | "MX" | "NAPTR" | "NS" | "PTR" | "SOA" | "SRV" | "TLSA" | "TXT";
  type DNSRecord = DNSAddress | DNSMX | DNSSRV | string | string[];
  type DNSAnswer = DNSAddress[] | DNSMX[] | DNSSRV[] | string[] | string[][];
  type EventCallback<Args extends unknown[] = unknown[]> = (...args: Args) => void;

  interface CodedError extends Error {
    code?: string | number;
    signal?: string | null;
    stdout?: string | Uint8Array;
    stderr?: string | Uint8Array;
  }

  interface FileInfo {
    readonly size: number;
    readonly mode: number;
    readonly mtimeMs: number;
    readonly mtime: string;
    isFile(): boolean;
    isDirectory(): boolean;
    isSymbolicLink(): boolean;
  }

  interface EventEmitter {
    on(name: PropertyKey, listener: EventCallback): this;
    addListener(name: PropertyKey, listener: EventCallback): this;
    prependListener(name: PropertyKey, listener: EventCallback): this;
    once(name: PropertyKey, listener: EventCallback): this;
    off(name: PropertyKey, listener: EventCallback): this;
    removeListener(name: PropertyKey, listener: EventCallback): this;
    removeAllListeners(name?: PropertyKey): this;
    emit(name: PropertyKey, ...args: unknown[]): boolean;
    listenerCount(name: PropertyKey): number;
    listeners(name: PropertyKey): EventCallback[];
    rawListeners(name: PropertyKey): EventCallback[];
    eventNames(): PropertyKey[];
    setMaxListeners(count: number): this;
    getMaxListeners(): number;
  }
  interface EventEmitterConstructor {
    new(): EventEmitter;
    readonly EventEmitter: EventEmitterConstructor;
  }
  function once(emitter: EventEmitter, name: PropertyKey): Promise<unknown[]>;

  interface AssertionError extends Error {
    actual?: unknown;
    expected?: unknown;
    operator: string;
    generatedMessage: boolean;
  }
  interface AssertionErrorConstructor {
    new(options: {message?: string; actual?: unknown; expected?: unknown; operator?: string}): AssertionError;
  }
  interface AssertionAPI {
    (value: unknown, message?: string): asserts value;
    readonly AssertionError: AssertionErrorConstructor;
    readonly ok: typeof assert;
    readonly fail: typeof fail;
    readonly equal: typeof equal;
    readonly notEqual: typeof notEqual;
    readonly strictEqual: typeof strictEqual;
    readonly notStrictEqual: typeof notStrictEqual;
    readonly deepEqual: typeof deepEqual;
    readonly deepStrictEqual: typeof deepStrictEqual;
    readonly notDeepStrictEqual: typeof notDeepStrictEqual;
    readonly match: typeof match;
    readonly doesNotMatch: typeof doesNotMatch;
    readonly throws: typeof throws;
    readonly doesNotThrow: typeof doesNotThrow;
    readonly rejects: typeof rejects;
    readonly doesNotReject: typeof doesNotReject;
    readonly strict: AssertionAPI;
  }
  function assert(value: unknown, message?: string): asserts value;
  function fail(message?: string): never;
  function equal(actual: unknown, expected: unknown, message?: string): void;
  function notEqual(actual: unknown, expected: unknown, message?: string): void;
  function strictEqual(actual: unknown, expected: unknown, message?: string): void;
  function notStrictEqual(actual: unknown, expected: unknown, message?: string): void;
  function deepEqual(actual: unknown, expected: unknown, message?: string): void;
  function deepStrictEqual(actual: unknown, expected: unknown, message?: string): void;
  function notDeepStrictEqual(actual: unknown, expected: unknown, message?: string): void;
  function match(value: string, pattern: RegExp, message?: string): void;
  function doesNotMatch(value: string, pattern: RegExp, message?: string): void;
  function throws(callback: () => unknown, expected?: unknown, message?: string): void;
  function doesNotThrow(callback: () => unknown, message?: string): void;
  function rejects(value: PromiseLike<unknown> | (() => PromiseLike<unknown>), expected?: unknown, message?: string): Promise<void>;
  function doesNotReject(value: PromiseLike<unknown> | (() => PromiseLike<unknown>), message?: string): Promise<void>;

  interface Buffer extends Uint8Array {
    toString(encoding?: BufferEncoding, start?: number, end?: number): string;
    toJSON(): {type: "Buffer"; data: number[]};
    equals(other: Uint8Array): boolean;
    write(text: string, offset?: number, encoding?: BufferEncoding): number;
  }
  interface BufferConstructor {
    new(size: number): Buffer;
    new(source: ArrayLike<number> | ArrayBuffer, byteOffset?: number, length?: number): Buffer;
    alloc(size: number, fill?: number | string): Buffer;
    allocUnsafe(size: number): Buffer;
    from(value: string, encoding?: BufferEncoding): Buffer;
    from(value: ArrayLike<number> | ArrayBuffer | ArrayBufferView, byteOffset?: number, length?: number): Buffer;
    concat(list: readonly Uint8Array[], totalLength?: number): Buffer;
    isBuffer(value: unknown): value is Buffer;
    byteLength(value: string | Uint8Array, encoding?: BufferEncoding): number;
  }

  interface AsyncLocalStorage<T> {
    getStore(): T | undefined;
    run<Args extends unknown[], Result>(store: T, callback: (...args: Args) => Result, ...args: Args): Result;
    exit<Args extends unknown[], Result>(callback: (...args: Args) => Result, ...args: Args): Result;
    enterWith(store: T): void;
    disable(): void;
  }
  interface AsyncLocalStorageConstructor {
    new<T = unknown>(): AsyncLocalStorage<T>;
    bind<Fn extends (...args: never[]) => unknown>(callback: Fn): Fn;
    snapshot(): <Args extends unknown[], Result>(callback: (...args: Args) => Result, ...args: Args) => Result;
  }
  interface AsyncResource {
    readonly type: string;
    runInAsyncScope<Args extends unknown[], Result>(callback: (...args: Args) => Result, thisArg?: unknown, ...args: Args): Result;
    bind<Fn extends (...args: never[]) => unknown>(callback: Fn, thisArg?: unknown): Fn;
    emitDestroy(): this;
    asyncId(): number;
    triggerAsyncId(): number;
  }
  interface AsyncResourceConstructor {
    new(type: string): AsyncResource;
    bind<Fn extends (...args: never[]) => unknown>(callback: Fn, type?: string, thisArg?: unknown): Fn;
  }
  interface AsyncHook { enable(): this; disable(): this }
  function createHook(callbacks?: Readonly<Record<string, unknown>>): AsyncHook;
  function executionAsyncId(): number;
  function executionAsyncResource(): object;
  function triggerAsyncId(): number;

  interface ExecOptions { cwd?: string; env?: Record<string, string>; input?: ByteInput; encoding?: string }
  type ExecOutput<Options> = Options extends {encoding: "buffer"} ? Uint8Array
    : Options extends object
      ? "encoding" extends keyof Options
        ? "buffer" extends (Options extends {encoding?: infer Encoding} ? Encoding : never)
          ? string | Uint8Array
          : string
        : string
      : string;
  interface ExecResult<Output = string> { stdout: Output; stderr: Output }
  interface SpawnSyncResult<Output = string> { status: number; stdout: Output; stderr: Output; signal: string | null; error?: Error }
  function execFile<const Options extends ExecOptions | undefined = undefined>(file: string, args?: readonly string[], options?: Options): Promise<ExecResult<ExecOutput<Options>>>;
  function exec<const Options extends ExecOptions | undefined = undefined>(command: string, options?: Options): Promise<ExecResult<ExecOutput<Options>>>;
  function execFileSync(file: string, args?: readonly string[], options?: Omit<ExecOptions, "encoding">): string;
  function execSync(command: string, options?: Omit<ExecOptions, "encoding">): string;
  function spawnSync<const Options extends ExecOptions | undefined = undefined>(file: string, args?: readonly string[], options?: Options): SpawnSyncResult<ExecOutput<Options>>;

  interface Hash { update(data: ByteInput): this; digest(): Buffer; digest(encoding: BufferEncoding): string }
  type RandomCallback = (error: Error | null, data?: Buffer) => void;
  type PBKDF2Callback = (error: Error | null, key?: Buffer) => void;
  type HKDFCallback = (error: Error | null, key?: ArrayBuffer) => void;
  type CryptoAlgorithm = string | {name: string; hash?: string | {name: string}};
  interface CryptoKey {
    readonly type: "secret";
    readonly algorithm: {readonly name: string; readonly hash?: {readonly name: string}};
    readonly extractable: boolean;
    readonly usages: readonly CryptoKeyUsage[];
  }
  type CryptoKeyUsage = "deriveBits" | "deriveKey" | "sign" | "verify";
  interface WebCryptoSubtle {
    digest(algorithm: string | {name: string}, data: ArrayBuffer | ArrayBufferView): Promise<ArrayBuffer>;
    importKey(format: "raw", data: ArrayBuffer | ArrayBufferView, algorithm: CryptoAlgorithm, extractable: boolean, usages: readonly CryptoKeyUsage[]): Promise<CryptoKey>;
    exportKey(format: "raw", key: CryptoKey): Promise<ArrayBuffer>;
    sign(algorithm: "HMAC" | {name: "HMAC"}, key: CryptoKey, data: ArrayBuffer | ArrayBufferView): Promise<ArrayBuffer>;
    verify(algorithm: "HMAC" | {name: "HMAC"}, key: CryptoKey, signature: ArrayBuffer | ArrayBufferView, data: ArrayBuffer | ArrayBufferView): Promise<boolean>;
    deriveBits(algorithm: CryptoAlgorithm & {salt: ArrayBuffer | ArrayBufferView; iterations?: number; info?: ArrayBuffer | ArrayBufferView}, key: CryptoKey, length: number): Promise<ArrayBuffer>;
    deriveKey(algorithm: CryptoAlgorithm & {salt: ArrayBuffer | ArrayBufferView; iterations?: number; info?: ArrayBuffer | ArrayBufferView}, baseKey: CryptoKey, derived: {name: string; length?: number}, extractable: boolean, usages: readonly CryptoKeyUsage[]): Promise<CryptoKey>;
  }
  interface Crypto {
    getRandomValues<T extends IntegerTypedArray>(view: T): T;
    randomUUID(): string;
    readonly subtle: WebCryptoSubtle;
  }
  function createHash(algorithm: string): Hash;
  function createHmac(algorithm: string, key: ByteInput): Hash;
  function randomBytes(size: number): Buffer;
  function randomBytes(size: number, callback: RandomCallback): void;
  function randomFillSync<T extends Uint8Array>(view: T, offset?: number, size?: number): T;
  function randomInt(min: number, max?: number): number;
  function randomInt(min: number, max: number, callback: (error: Error | null, value?: number) => void): void;
  function randomInt(max: number, callback: (error: Error | null, value?: number) => void): void;
  function pbkdf2(password: ByteInput, salt: ByteInput, iterations: number, keyLength: number, callback: PBKDF2Callback): void;
  function pbkdf2(password: ByteInput, salt: ByteInput, iterations: number, keyLength: number, digest: string, callback: PBKDF2Callback): void;
  function pbkdf2Sync(password: ByteInput, salt: ByteInput, iterations: number, keyLength: number, digest?: string): Buffer;
  function hkdf(digest: string, key: ByteInput, salt: ByteInput, info: ByteInput, keyLength: number, callback: HKDFCallback): void;
  function hkdfSync(digest: string, key: ByteInput, salt: ByteInput, info: ByteInput, keyLength: number): Buffer;
  function timingSafeEqual(a: ArrayBufferView, b: ArrayBufferView): boolean;
  function getHashes(): string[];
  const cryptoConstants: Readonly<Record<string, never>>;

  interface DNSAddress { address: string; family: number }
  interface DNSService { hostname: string; service: string }
  interface DNSMX { exchange: string; priority: number }
  interface DNSSRV { name: string; port: number; priority: number; weight: number }
  interface DNSLookupOptions { family?: DNSFamily; all?: boolean; order?: DNSResultOrder; verbatim?: boolean }
  interface DNSResolveOptions { ttl?: boolean }
  interface DNSFailure extends Error { code: string; syscall: string; hostname: string }
  type DNSLookupResult = DNSAddress | {address: null; family: number} | undefined;
  type DNSLookupCallback = (error: DNSFailure | null, address: string | null | DNSAddress[] | undefined, family?: number) => void;
  type DNSLookupServiceCallback = (error: DNSFailure | null, hostname?: string, service?: string) => void;
  type DNSQueryCallback = (error: DNSFailure | null, records?: DNSAnswer) => void;
  type DNSAddressListCallback = (error: DNSFailure | null, records?: string[]) => void;
  interface DNSPromiseQueries {
    resolve(hostname: string, type?: DNSRecordType): Promise<DNSAnswer>;
    reverse(address: string): Promise<string[]>;
    resolve4(hostname: string, options?: DNSResolveOptions): Promise<string[]>;
    resolve6(hostname: string, options?: DNSResolveOptions): Promise<string[]>;
    resolveAny(hostname: string, options?: DNSResolveOptions): Promise<DNSAnswer>;
    resolveCaa(hostname: string, options?: DNSResolveOptions): Promise<DNSAnswer>;
    resolveCname(hostname: string, options?: DNSResolveOptions): Promise<string[]>;
    resolveMx(hostname: string, options?: DNSResolveOptions): Promise<DNSMX[]>;
    resolveNaptr(hostname: string, options?: DNSResolveOptions): Promise<DNSAnswer>;
    resolveNs(hostname: string, options?: DNSResolveOptions): Promise<string[]>;
    resolvePtr(hostname: string, options?: DNSResolveOptions): Promise<string[]>;
    resolveSoa(hostname: string, options?: DNSResolveOptions): Promise<DNSAnswer>;
    resolveSrv(hostname: string, options?: DNSResolveOptions): Promise<DNSSRV[]>;
    resolveTlsa(hostname: string, options?: DNSResolveOptions): Promise<DNSAnswer>;
    resolveTxt(hostname: string, options?: DNSResolveOptions): Promise<string[][]>;
    getServers(): string[];
    setServers(servers: readonly string[]): void;
  }
  interface PromiseResolver extends DNSPromiseQueries {
    cancel(): void;
    setLocalAddress(): void;
  }
  interface PromiseResolverConstructor { new(options?: object): PromiseResolver }
  interface DNSPromisesAPI extends DNSPromiseQueries {
    lookup(hostname: string, options?: DNSFamily | DNSLookupOptions): Promise<DNSLookupResult | DNSAddress[]>;
    lookupService(address: string, port: number): Promise<DNSService>;
    getDefaultResultOrder(): DNSResultOrder;
    setDefaultResultOrder(order: DNSResultOrder): void;
    readonly Resolver: PromiseResolverConstructor;
    readonly ADDRGETNETWORKPARAMS: number;
    readonly BADFAMILY: number; readonly BADFLAGS: number; readonly BADHINTS: number; readonly BADNAME: number;
    readonly BADQUERY: number; readonly BADRESP: number; readonly BADSTR: number; readonly CANCELLED: number;
    readonly CONNREFUSED: number; readonly DESTRUCTION: number; readonly EOF: number; readonly FILE: number;
    readonly FORMERR: number; readonly LOADIPHLPAPI: number; readonly NODATA: number; readonly NOMEM: number;
    readonly NONAME: number; readonly NOTFOUND: number; readonly NOTIMP: number; readonly NOTINITIALIZED: number;
    readonly REFUSED: number; readonly SERVFAIL: number; readonly TIMEOUT: number;
  }
  interface DNSCallbackQueries {
    resolve(hostname: string, type: DNSRecordType | undefined, callback: DNSQueryCallback): void;
    resolve(hostname: string, callback: DNSQueryCallback): void;
    reverse(address: string, callback: DNSQueryCallback): void;
    resolve4(hostname: string, callback: DNSAddressListCallback): void;
    resolve4(hostname: string, options: DNSResolveOptions, callback: DNSAddressListCallback): void;
    resolve6(hostname: string, callback: DNSAddressListCallback): void;
    resolve6(hostname: string, options: DNSResolveOptions, callback: DNSAddressListCallback): void;
    resolveAny(hostname: string, callback: DNSQueryCallback): void;
    resolveAny(hostname: string, options: DNSResolveOptions, callback: DNSQueryCallback): void;
    resolveCaa(hostname: string, callback: DNSQueryCallback): void;
    resolveCaa(hostname: string, options: DNSResolveOptions, callback: DNSQueryCallback): void;
    resolveCname(hostname: string, callback: DNSQueryCallback): void;
    resolveCname(hostname: string, options: DNSResolveOptions, callback: DNSQueryCallback): void;
    resolveMx(hostname: string, callback: DNSQueryCallback): void;
    resolveMx(hostname: string, options: DNSResolveOptions, callback: DNSQueryCallback): void;
    resolveNaptr(hostname: string, callback: DNSQueryCallback): void;
    resolveNaptr(hostname: string, options: DNSResolveOptions, callback: DNSQueryCallback): void;
    resolveNs(hostname: string, callback: DNSQueryCallback): void;
    resolveNs(hostname: string, options: DNSResolveOptions, callback: DNSQueryCallback): void;
    resolvePtr(hostname: string, callback: DNSQueryCallback): void;
    resolvePtr(hostname: string, options: DNSResolveOptions, callback: DNSQueryCallback): void;
    resolveSoa(hostname: string, callback: DNSQueryCallback): void;
    resolveSoa(hostname: string, options: DNSResolveOptions, callback: DNSQueryCallback): void;
    resolveSrv(hostname: string, callback: DNSQueryCallback): void;
    resolveSrv(hostname: string, options: DNSResolveOptions, callback: DNSQueryCallback): void;
    resolveTlsa(hostname: string, callback: DNSQueryCallback): void;
    resolveTlsa(hostname: string, options: DNSResolveOptions, callback: DNSQueryCallback): void;
    resolveTxt(hostname: string, callback: DNSQueryCallback): void;
    resolveTxt(hostname: string, options: DNSResolveOptions, callback: DNSQueryCallback): void;
  }
  interface DNSCallbackResolver extends DNSCallbackQueries {
    getServers(): string[];
    setServers(servers: readonly string[]): void;
    cancel(): void;
    setLocalAddress(): void;
  }
  interface DNSCallbackAPI extends DNSCallbackQueries {
    lookup(hostname: string, callback: DNSLookupCallback): void;
    lookup(hostname: string, options: DNSFamily | DNSLookupOptions, callback: DNSLookupCallback): void;
    lookupService(address: string, port: number, callback: DNSLookupServiceCallback): void;
    getServers(): string[];
    setServers(servers: readonly string[]): void;
    getDefaultResultOrder(): DNSResultOrder;
    setDefaultResultOrder(order: DNSResultOrder): void;
    readonly promises: DNSPromisesAPI;
    readonly Resolver: {new(options?: object): DNSCallbackResolver};
    readonly ADDRCONFIG: number; readonly ADDRGETNETWORKPARAMS: number; readonly ALL: number;
    readonly BADFAMILY: number; readonly BADFLAGS: number; readonly BADHINTS: number; readonly BADNAME: number;
    readonly BADQUERY: number; readonly BADRESP: number; readonly BADSTR: number; readonly CANCELLED: number;
    readonly CONNREFUSED: number; readonly DESTRUCTION: number; readonly EOF: number; readonly FILE: number;
    readonly FORMERR: number; readonly LOADIPHLPAPI: number; readonly NODATA: number; readonly NOMEM: number;
    readonly NONAME: number; readonly NOTFOUND: number; readonly NOTIMP: number; readonly NOTINITIALIZED: number;
    readonly REFUSED: number; readonly SERVFAIL: number; readonly TIMEOUT: number; readonly V4MAPPED: number;
  }

  interface FileReadOptions { start?: number; end?: number }
  interface FileWriteOptions { flags?: string }
  type FileData = string | Uint8Array | ArrayBuffer | ArrayBufferView;
  interface FSModule {
    appendFileSync(path: string, data: FileData): void;
    chmodSync(path: string, mode: number): void;
    copyFileSync(from: string, to: string): void;
    createReadStream(path: string, options?: FileReadOptions): ReadableStream<Uint8Array>;
    createWriteStream(path: string, options?: FileWriteOptions): WritableStream<Uint8Array | string>;
    existsSync(path: string): boolean;
    lstatSync(path: string): FileInfo;
    mkdirSync(path: string, options?: {recursive?: boolean}): void;
    mkdtempSync(prefix: string): string;
    readFileSync(path: string): Uint8Array;
    readFileSync(path: string, encoding: string | {encoding: string}): string;
    readdirSync(path: string): string[];
    readlinkSync(path: string): string;
    realpathSync(path: string): string;
    renameSync(from: string, to: string): void;
    rmSync(path: string, options?: {recursive?: boolean; force?: boolean}): void;
    rmdirSync(path: string): void;
    statSync(path: string): FileInfo;
    unlinkSync(path: string): void;
    writeFileSync(path: string, data: FileData): void;
    readonly promises: FSPromisesAPI;
  }
  interface FSPromisesAPI {
    appendFile(path: string, data: FileData): Promise<void>;
    copyFile(from: string, to: string): Promise<void>;
    lstat(path: string): Promise<FileInfo>;
    mkdir(path: string, options?: {recursive?: boolean}): Promise<void>;
    mkdtemp(prefix: string): Promise<string>;
    readFile(path: string): Promise<Uint8Array>;
    readFile(path: string, encoding: string | {encoding: string}): Promise<string>;
    readdir(path: string): Promise<string[]>;
    readlink(path: string): Promise<string>;
    realpath(path: string): Promise<string>;
    rename(from: string, to: string): Promise<void>;
    rm(path: string, options?: {recursive?: boolean; force?: boolean}): Promise<void>;
    rmdir(path: string): Promise<void>;
    stat(path: string): Promise<FileInfo>;
    unlink(path: string): Promise<void>;
    writeFile(path: string, data: FileData): Promise<void>;
  }

  interface ServeOptions { hostname?: string; port?: number; onError?: (error: unknown) => Response | PromiseLike<Response> }
  type FetchHandler = (request: Request) => Response | PromiseLike<Response>;
  interface HTTPServer { readonly hostname: string; readonly port: number; readonly url: string; close(): void }
  function serve(handler: FetchHandler): HTTPServer;
  function serve(options: ServeOptions & {fetch?: FetchHandler}, handler?: FetchHandler): HTTPServer;

  interface OSModule {
    readonly EOL: string;
    arch(): string;
    cpus(): number;
    homedir(): string;
    hostname(): string;
    platform(): string;
    tmpdir(): string;
    type(): string;
  }

  interface PathObject { root: string; dir: string; base: string; ext: string; name: string }
  interface PathFormatObject { root?: string; dir?: string; base?: string; ext?: string; name?: string }
  interface PathAPI {
    readonly delimiter: string;
    readonly sep: string;
    readonly posix: PathAPI;
    readonly win32: PathAPI;
    basename(path: string, suffix?: string): string;
    dirname(path: string): string;
    extname(path: string): string;
    format(path: PathFormatObject): string;
    isAbsolute(path: string): boolean;
    join(...paths: string[]): string;
    normalize(path: string): string;
    parse(path: string): PathObject;
    relative(from: string, to: string): string;
    resolve(...paths: string[]): string;
    toNamespacedPath(path: string): string;
    _makeLong(path: string): string;
  }

  interface ProcessStreamOut { write(text: string): boolean; readonly isTTY: false }
  interface ProcessStreamIn { read(): string | null; readonly isTTY: false }
  interface Process {
    readonly argv: string[];
    readonly env: Record<string, string>;
    readonly platform: string;
    readonly arch: string;
    readonly version: string;
    readonly pid: number;
    exitCode?: number;
    cwd(): string;
    uptime(): number;
    hrtime(previous?: readonly [number, number]): [number, number];
    exit(code?: number): void;
    nextTick<Args extends unknown[]>(callback: (...args: Args) => void, ...args: Args): void;
    on(name: PropertyKey, listener: EventCallback): this;
    once(name: PropertyKey, listener: EventCallback): this;
    off(name: PropertyKey, listener: EventCallback): this;
    removeListener(name: PropertyKey, listener: EventCallback): this;
    emit(name: PropertyKey, ...args: unknown[]): boolean;
    listeners(name: PropertyKey): EventCallback[];
    readonly stdout?: ProcessStreamOut;
    readonly stderr?: ProcessStreamOut;
    readonly stdin?: ProcessStreamIn;
  }

  interface QueryStringRecord { [key: string]: string | string[] }
  interface QueryStringAPI {
    decode: typeof parseQuery;
    encode: typeof stringifyQuery;
    escape(value: unknown): string;
    parse(value: string, separator?: string, equals?: string): QueryStringRecord;
    stringify(value: Readonly<Record<string, string | number | boolean | readonly (string | number | boolean)[]>>, separator?: string, equals?: string): string;
    unescape(value: string): string;
  }
  function parseQuery(value: string, separator?: string, equals?: string): QueryStringRecord;
  function stringifyQuery(value: Readonly<Record<string, string | number | boolean | readonly (string | number | boolean)[]>>, separator?: string, equals?: string): string;

  interface ReadableStreamReadDoneResult { readonly done: true; readonly value?: undefined }
  interface ReadableStreamReadValueResult<T> { readonly done: false; readonly value: T }
  type ReadableStreamReadResult<T> = ReadableStreamReadDoneResult | ReadableStreamReadValueResult<T>;
  interface ReadableStreamReadOptions { mode?: undefined }
  interface ReadableStreamPipeOptions { preventAbort?: boolean; preventCancel?: boolean; preventClose?: boolean; signal?: AbortSignal }
  interface QueuingStrategy<T> { highWaterMark?: number; size?(chunk: T): number }
  interface ReadableStreamSource<T> {
    start?(controller: ReadableStreamDefaultController<T>): unknown;
    pull?(controller: ReadableStreamDefaultController<T>): unknown;
    cancel?(reason?: unknown): unknown;
    type?: undefined;
  }
  interface WritableStreamSink<T> {
    start?(controller: WritableStreamDefaultController): unknown;
    write?(chunk: T, controller: WritableStreamDefaultController): unknown;
    close?(): unknown;
    abort?(reason?: unknown): unknown;
    type?: undefined;
  }
  interface TransformStreamTransformer<I, O> {
    start?(controller: TransformStreamDefaultController<O>): unknown;
    transform?(chunk: I, controller: TransformStreamDefaultController<O>): unknown;
    flush?(controller: TransformStreamDefaultController<O>): unknown;
  }
  interface TransformStreamDefaultController<T = unknown> {
    readonly desiredSize: number | null;
    enqueue(chunk: T): void;
    error(reason?: unknown): void;
    terminate(): void;
  }
  interface ReadableStream<T = unknown> extends AsyncIterable<T> {
    readonly locked: boolean;
    cancel(reason?: unknown): Promise<void>;
    getReader(options?: ReadableStreamReadOptions): ReadableStreamDefaultReader<T>;
    pipeThrough<O>(transform: {readable: ReadableStream<O>; writable: WritableStream<T>}, options?: ReadableStreamPipeOptions): ReadableStream<O>;
    pipeTo(destination: WritableStream<T>, options?: ReadableStreamPipeOptions): Promise<void>;
    tee(): [ReadableStream<T>, ReadableStream<T>];
    values(options?: {preventCancel?: boolean}): AsyncIterableIterator<T>;
  }
  interface ReadableStreamConstructor {
    new<T = unknown>(source?: ReadableStreamSource<T>, strategy?: QueuingStrategy<T>): ReadableStream<T>;
    from<T>(iterable: Iterable<T> | AsyncIterable<T>): ReadableStream<T>;
  }
  interface ReadableStreamDefaultReader<T = unknown> {
    readonly closed: Promise<void>;
    read(): Promise<ReadableStreamReadResult<T>>;
    cancel(reason?: unknown): Promise<void>;
    releaseLock(): void;
  }
  interface ReadableStreamDefaultReaderConstructor { new<T = unknown>(stream: ReadableStream<T>): ReadableStreamDefaultReader<T> }
  interface ReadableStreamDefaultController<T = unknown> {
    readonly desiredSize: number | null;
    enqueue(chunk: T): void;
    close(): void;
    error(reason?: unknown): void;
  }
  interface ReadableStreamDefaultControllerConstructor { new<T = unknown>(stream: ReadableStream<T>, source?: ReadableStreamSource<T>, strategy?: QueuingStrategy<T>): ReadableStreamDefaultController<T> }
  interface WritableStream<T = unknown> {
    readonly locked: boolean;
    abort(reason?: unknown): Promise<void>;
    close(): Promise<void>;
    getWriter(): WritableStreamDefaultWriter<T>;
  }
  interface WritableStreamConstructor { new<T = unknown>(sink?: WritableStreamSink<T>, strategy?: QueuingStrategy<T>): WritableStream<T> }
  interface WritableStreamDefaultController { error(reason?: unknown): void }
  interface WritableStreamDefaultWriter<T = unknown> {
    readonly closed: Promise<void>;
    readonly desiredSize: number | null;
    readonly ready: Promise<void>;
    abort(reason?: unknown): Promise<void>;
    close(): Promise<void>;
    write(chunk: T): Promise<void>;
    releaseLock(): void;
  }
  interface WritableStreamDefaultWriterConstructor { new<T = unknown>(stream: WritableStream<T>): WritableStreamDefaultWriter<T> }
  interface TransformStream<I = unknown, O = unknown> { readonly readable: ReadableStream<O>; readonly writable: WritableStream<I> }
  interface TransformStreamConstructor { new<I = unknown, O = unknown>(transformer?: TransformStreamTransformer<I, O>, writableStrategy?: QueuingStrategy<I>, readableStrategy?: QueuingStrategy<O>): TransformStream<I, O> }
  interface TextEncoderStream extends TransformStream<string, Uint8Array> { readonly encoding: "utf-8" }
  interface TextEncoderStreamConstructor { new(): TextEncoderStream }
  interface TextDecoderStream extends TransformStream<Uint8Array, string> { readonly encoding: "utf-8" }
  interface TextDecoderStreamConstructor { new(label?: "utf-8" | "utf8" | "unicode-1-1-utf-8", options?: {fatal?: boolean; ignoreBOM?: boolean}): TextDecoderStream }
  interface QueuingStrategyObject<T> { readonly highWaterMark: number; size(chunk: T): number }
  interface QueuingStrategyConstructor<T> { new(init?: {highWaterMark?: number}): QueuingStrategyObject<T> }

  interface StringDecoder { readonly encoding: string; write(bytes: Uint8Array | string): string; end(bytes?: Uint8Array | string): string }
  interface StringDecoderConstructor { new(encoding?: "utf8" | "utf-8"): StringDecoder }

  interface TimerPromiseOptions { signal?: AbortSignal }
  interface TimerPromisesAPI {
    setTimeout<T = void>(delay?: number, value?: T, options?: TimerPromiseOptions): Promise<T>;
    setImmediate<T = void>(value?: T, options?: TimerPromiseOptions): Promise<T>;
    setInterval<T = void>(delay: number, value?: T, options?: TimerPromiseOptions): AsyncIterableIterator<T>;
    readonly scheduler: {wait(delay: number, options?: TimerPromiseOptions): Promise<void>; yield(): Promise<void>};
  }
  interface TimersAPI {
    setTimeout<Args extends unknown[]>(callback: (...args: Args) => void, delay?: number, ...args: Args): number;
    clearTimeout(id: number): void;
    setInterval<Args extends unknown[]>(callback: (...args: Args) => void, delay?: number, ...args: Args): number;
    clearInterval(id: number): void;
    setImmediate<Args extends unknown[]>(callback: (...args: Args) => void, ...args: Args): number;
    clearImmediate(id: number): void;
  }

  interface URLSearchParams extends Iterable<[string, string]> {
    append(name: string, value: string): void;
    delete(name: string): void;
    get(name: string): string | null;
    getAll(name: string): string[];
    has(name: string): boolean;
    set(name: string, value: string): void;
    sort(): void;
    toString(): string;
    entries(): IterableIterator<[string, string]>;
    keys(): IterableIterator<string>;
    values(): IterableIterator<string>;
    forEach(callback: (value: string, key: string, params: URLSearchParams) => void, thisArg?: unknown): void;
  }
  type URLSearchParamsInit = string | URLSearchParams | Iterable<readonly [string, string]> | Readonly<Record<string, string>>;
  interface URLSearchParamsConstructor { new(init?: URLSearchParamsInit): URLSearchParams }
  interface URL {
    href: string;
    protocol: string;
    username: string;
    password: string;
    host: string;
    hostname: string;
    port: string;
    pathname: string;
    search: string;
    readonly searchParams: URLSearchParams;
    hash: string;
    readonly origin: string;
    toString(): string;
    toJSON(): string;
  }
  interface URLConstructor {
    new(input: string | URL, base?: string | URL): URL;
    canParse(input: string | URL, base?: string | URL): boolean;
    parse(input: string | URL, base?: string | URL): URL | null;
  }
  interface URLModule {
    readonly URL: URLConstructor;
    readonly URLSearchParams: URLSearchParamsConstructor;
    domainToASCII(domain: string): string;
    domainToUnicode(domain: string): string;
    fileURLToPath(url: string | URL): string;
    format(url: string | URL): string;
    parse(url: string): URL;
    pathToFileURL(path: string): URL;
    resolve(from: string, to: string): string;
  }

  interface UtilTypes {
    isDate(value: unknown): value is Date;
    isRegExp(value: unknown): value is RegExp;
    isMap(value: unknown): value is Map<unknown, unknown>;
    isSet(value: unknown): value is Set<unknown>;
    isPromise(value: unknown): value is Promise<unknown>;
    isTypedArray(value: unknown): value is IntegerTypedArray | Float32Array | Float64Array;
    isArrayBuffer(value: unknown): value is ArrayBuffer;
    isNativeError(value: unknown): value is Error;
  }
  type CallbackError = Error | null;
  function format(...values: unknown[]): string;
  function inspect(value: unknown): string;
  function promisify<Args extends unknown[], Result>(fn: (...args: [...Args, (error: CallbackError, result: Result) => void]) => unknown): (...args: Args) => Promise<Result>;
  function callbackify<Args extends unknown[], Result>(fn: (...args: Args) => PromiseLike<Result>): (...args: [...Args, (error: CallbackError, result?: Result) => void]) => void;
  function deprecate<Fn extends (...args: never[]) => unknown>(fn: Fn): Fn;
  function inherits(constructor: Function, superConstructor: Function): void;
  function isDeepStrictEqual(a: unknown, b: unknown): boolean;
  const utilTypes: UtilTypes;

  interface VMContext { readonly global: object; run(code: string, filename: string, lineOffset: number, columnOffset: number, timeout: number): unknown }
  interface VMConstants { readonly USE_MAIN_CONTEXT_DEFAULT_LOADER: symbol; readonly DONT_CONTEXTIFY: symbol }
  interface VMRunOptions { filename?: string; lineOffset?: number; columnOffset?: number; timeout?: number }
  interface VMCompileOptions extends VMRunOptions { parsingContext?: object; contextExtensions?: object[] }
  interface Script {
    runInThisContext(options?: VMRunOptions | string): unknown;
    runInContext(context: object, options?: VMRunOptions): unknown;
    runInNewContext(context?: object, options?: VMRunOptions): unknown;
  }
  interface ScriptConstructor { new(code: string, options?: VMRunOptions | string): Script }
  interface VMModule {
    readonly Script: ScriptConstructor;
    readonly constants: VMConstants;
    compileFunction(code: string, params?: readonly string[], options?: VMCompileOptions): (...args: unknown[]) => unknown;
    createContext(context?: object | typeof vmConstants.DONT_CONTEXTIFY, options?: object): object;
    createScript(code: string, options?: VMRunOptions | string): Script;
    isContext(value: object): boolean;
    measureMemory(): Promise<never>;
    runInContext(code: string, context: object, options?: VMRunOptions | string): unknown;
    runInNewContext(code: string, context?: object, options?: VMRunOptions | string): unknown;
    runInThisContext(code: string, options?: VMRunOptions | string): unknown;
  }
  const vmConstants: VMConstants;

  interface WorkerDataOptions { workerData?: unknown; transferList?: Iterable<Transferable> }
  const SHARE_ENV: unique symbol;
  interface WorkerThreadOptions extends WorkerDataOptions {
    argv?: readonly string[];
    env?: Record<string, string> | typeof SHARE_ENV;
    eval?: boolean;
    name?: string;
  }
  interface NodeWorker extends EventEmitter {
    readonly threadId: number;
    postMessage(value: unknown, transfer?: Iterable<Transferable> | {transfer: Iterable<Transferable>}): void;
    /** Resolves to the exit code, or undefined when the Worker already exited. */
    terminate(): Promise<number | undefined>;
    ref(): void;
    unref(): void;
  }
  interface NodeWorkerConstructor { new(filename: string | URL, options?: WorkerThreadOptions): NodeWorker }
  interface WorkerThreadsAPI {
    readonly BroadcastChannel: BroadcastChannelConstructor;
    readonly MessageChannel: MessageChannelConstructor;
    readonly MessagePort: MessagePortConstructor;
    readonly SHARE_ENV: typeof SHARE_ENV;
    readonly Worker: NodeWorkerConstructor;
    readonly getEnvironmentData: (key: unknown) => unknown;
    readonly isMainThread: boolean;
    readonly isMarkedAsUntransferable: (value: unknown) => boolean;
    readonly markAsUntransferable: (value: unknown) => void;
    readonly parentPort: MessagePort | null;
    readonly receiveMessageOnPort: (port: MessagePort) => {message: unknown} | undefined;
    readonly resourceLimits: Readonly<Record<string, never>>;
    readonly setEnvironmentData: (key: unknown, value: unknown) => void;
    readonly threadId: number;
    readonly workerData: unknown;
  }

  interface ZlibPacker { write(data: Uint8Array | ArrayBuffer): Uint8Array; close(): Uint8Array }
  interface ZlibOptions { encoding?: string }
  type ZlibCallback = (error: Error | null, result?: Uint8Array) => void;
  type ZlibSyncFunction = (data: Uint8Array | ArrayBuffer) => Uint8Array;
  type ZlibAsyncFunction = (data: Uint8Array | ArrayBuffer, callback: ZlibCallback) => void;
  interface ZlibAPI {
    readonly constants: Readonly<Record<string, number>>;
    createDeflate(): CompressionStream;
    createDeflateRaw(): CompressionStream;
    createGunzip(): DecompressionStream;
    createGzip(): CompressionStream;
    createInflate(): DecompressionStream;
    createInflateRaw(): DecompressionStream;
    readonly deflate: ZlibAsyncFunction;
    readonly deflateRaw: ZlibAsyncFunction;
    readonly deflateRawSync: ZlibSyncFunction;
    readonly deflateSync: ZlibSyncFunction;
    readonly gunzip: ZlibAsyncFunction;
    readonly gunzipSync: ZlibSyncFunction;
    readonly gzip: ZlibAsyncFunction;
    readonly gzipSync: ZlibSyncFunction;
    readonly inflate: ZlibAsyncFunction;
    readonly inflateRaw: ZlibAsyncFunction;
    readonly inflateRawSync: ZlibSyncFunction;
    readonly inflateSync: ZlibSyncFunction;
    unzip(data: Uint8Array | ArrayBuffer, callback: ZlibCallback): void;
    unzipSync(data: Uint8Array | ArrayBuffer): Uint8Array;
  }

  interface FileHandleRead { data: Uint8Array; bytesRead: number; eof: boolean }
  interface FileHandle { readAt(position: number, length: number): Promise<FileHandleRead>; writeAt(position: number, bytes: Uint8Array): Promise<number>; close(): Promise<void> }
  type FileHandleMode = "r" | "r+";
  function open(path: string, options?: {mode?: FileHandleMode}): Promise<FileHandle>;

  interface ChildProcessStatus { code: number | null; signal: string | null }
  interface OwnedChildProcess {
    readonly pid: number;
    readonly stdin: WritableStream<Uint8Array> | null;
    readonly stdout: ReadableStream<Uint8Array> | null;
    readonly stderr: ReadableStream<Uint8Array> | null;
    readonly exited: Promise<ChildProcessStatus>;
    kill(signal?: string): Promise<void>;
    close(): Promise<void>;
  }
  type ChildStreamMode = "pipe" | "inherit" | "ignore";
  interface SpawnOptions { stdin?: ChildStreamMode; stdout?: ChildStreamMode; stderr?: ChildStreamMode; cwd?: string; env?: Record<string, string | null> }
  function spawn(command: string, args?: readonly string[], options?: SpawnOptions): Promise<OwnedChildProcess>;

  interface TcpAddress { readonly network: "tcp"; readonly host: string; readonly port: number }
  interface UnixAddress { readonly network: "unix"; readonly path: string }
  type SocketAddress = TcpAddress | UnixAddress;
  interface TcpSocketOptions { network: "tcp"; host: string; port: number; signal?: AbortSignal }
  interface UnixSocketOptions { network: "unix"; path: string; signal?: AbortSignal }
  type ConnectOptions = TcpSocketOptions | UnixSocketOptions;
  type ListenOptions = (Omit<TcpSocketOptions, "port"> & {port: number}) | UnixSocketOptions;
  interface RawConnection { readonly readable: ReadableStream<Uint8Array>; readonly writable: WritableStream<Uint8Array>; close(): Promise<void> }
  interface SocketListener { readonly address: SocketAddress; accept(): Promise<RawConnection>; close(): Promise<void> }
  function connect(options: ConnectOptions): Promise<RawConnection>;
  function listen(options: ListenOptions): Promise<SocketListener>;

  interface SignalIteratorResult { readonly value: SignalName | undefined; readonly done: boolean }
  interface SignalSubscription extends AsyncIterableIterator<SignalName> {
    next(): Promise<IteratorResult<SignalName>>;
    return(value?: unknown): Promise<IteratorResult<SignalName>>;
    close(): Promise<void>;
  }
  function subscribe(name: SignalName): SignalSubscription;

  function docs(topic?: string, member?: string): string;
  function pipe(source: ReadableStream<Uint8Array>, destination: WritableStream<Uint8Array>, options?: {signal?: AbortSignal; preventClose?: boolean}): Promise<void>;

  interface URLPatternPartResult { input: string; groups: Record<string, string | undefined> }
  interface URLPatternResult {
    inputs: unknown[];
    protocol: URLPatternPartResult;
    username: URLPatternPartResult;
    password: URLPatternPartResult;
    hostname: URLPatternPartResult;
    port: URLPatternPartResult;
    pathname: URLPatternPartResult;
    search: URLPatternPartResult;
    hash: URLPatternPartResult;
  }
  interface URLPatternInit { protocol?: string; username?: string; password?: string; hostname?: string; port?: string; pathname?: string; search?: string; hash?: string }
  interface URLPattern {
    protocol: string; username: string; password: string; hostname: string;
    port: string; pathname: string; search: string; hash: string;
    test(input: string | URL, baseURL?: string): boolean;
    exec(input: string | URL, baseURL?: string): URLPatternResult | null;
  }
  interface URLPatternConstructor { new(input?: string | URLPatternInit, baseURL?: string): URLPattern }

  interface TextEncoder { readonly encoding: "utf-8"; encode(input?: string): Uint8Array; encodeInto(source: string, destination: Uint8Array): {read: number; written: number} }
  interface TextEncoderConstructor { new(): TextEncoder }
  interface TextDecoder { readonly encoding: "utf-8"; readonly fatal: boolean; readonly ignoreBOM: boolean; decode(input?: ArrayBuffer | ArrayBufferView, options?: {stream?: boolean}): string }
  interface TextDecoderConstructor { new(label?: "utf-8" | "utf8" | "unicode-1-1-utf-8", options?: {fatal?: boolean; ignoreBOM?: boolean}): TextDecoder }

  type BlobPart = string | ArrayBuffer | ArrayBufferView | Blob;
  interface Blob { readonly size: number; readonly type: string; arrayBuffer(): Promise<ArrayBuffer>; bytes(): Promise<Uint8Array>; text(): Promise<string>; slice(start?: number, end?: number, type?: string): Blob; stream(): ReadableStream<Uint8Array> }
  interface BlobConstructor { new(parts?: Iterable<BlobPart>, options?: {type?: string}): Blob }
  interface File extends Blob { readonly name: string; readonly lastModified: number }
  interface FileConstructor { new(parts: Iterable<BlobPart>, name: string, options?: {type?: string; lastModified?: number}): File }
  type FormDataEntryValue = string | File;
  interface FormData extends Iterable<[string, FormDataEntryValue]> {
    append(name: string, value: string | Blob, filename?: string): void;
    delete(name: string): void;
    get(name: string): FormDataEntryValue | null;
    getAll(name: string): FormDataEntryValue[];
    has(name: string): boolean;
    set(name: string, value: string | Blob, filename?: string): void;
    entries(): IterableIterator<[string, FormDataEntryValue]>;
    keys(): IterableIterator<string>;
    values(): IterableIterator<FormDataEntryValue>;
    forEach(callback: (value: FormDataEntryValue, key: string, form: FormData) => void, thisArg?: unknown): void;
  }
  interface FormDataConstructor { new(): FormData }

  interface EventInit { cancelable?: boolean }
  interface Event {
    readonly type: string;
    readonly cancelable: boolean;
    defaultPrevented: boolean;
    target: EventTarget | null;
    preventDefault(): void;
    stopPropagation(): void;
    stopImmediatePropagation(): void;
  }
  interface EventConstructor { new(type: string, init?: EventInit): Event }
  type EventListener<E extends Event = Event> = (this: EventTarget, event: E) => unknown;
  interface EventListenerObject<E extends Event = Event> { handleEvent(event: E): void }
  type EventListenerOrObject<E extends Event = Event> = EventListener<E> | EventListenerObject<E> | null;
  interface EventTarget {
    addEventListener(type: string, listener: EventListenerOrObject, options?: {once?: boolean}): void;
    removeEventListener(type: string, listener: EventListenerOrObject): void;
    dispatchEvent(event: Event): boolean;
  }
  interface EventTargetConstructor { new(): EventTarget }
  interface AbortSignal extends EventTarget {
    readonly aborted: boolean;
    readonly reason: unknown;
    onabort: EventListener<NOEStdlib.Event> | null;
    throwIfAborted(): void;
  }
  interface AbortSignalConstructor {
    new(): AbortSignal;
    abort(reason?: unknown): AbortSignal;
    timeout(milliseconds: number): AbortSignal;
  }
  interface AbortController { readonly signal: AbortSignal; abort(reason?: unknown): void }
  interface AbortControllerConstructor { new(): AbortController }
  interface DOMException extends Error { readonly code: number }
  interface DOMExceptionConstructor {
    new(message?: string, options?: string | {name?: string; cause?: unknown}): DOMException;
    readonly INDEX_SIZE_ERR: number; readonly DOMSTRING_SIZE_ERR: number; readonly HIERARCHY_REQUEST_ERR: number;
    readonly WRONG_DOCUMENT_ERR: number; readonly INVALID_CHARACTER_ERR: number; readonly NO_DATA_ALLOWED_ERR: number;
    readonly NO_MODIFICATION_ALLOWED_ERR: number; readonly NOT_FOUND_ERR: number; readonly NOT_SUPPORTED_ERR: number;
    readonly INUSE_ATTRIBUTE_ERR: number; readonly INVALID_STATE_ERR: number; readonly SYNTAX_ERR: number;
    readonly INVALID_MODIFICATION_ERR: number; readonly NAMESPACE_ERR: number; readonly INVALID_ACCESS_ERR: number;
    readonly VALIDATION_ERR: number; readonly TYPE_MISMATCH_ERR: number; readonly SECURITY_ERR: number;
    readonly NETWORK_ERR: number; readonly ABORT_ERR: number; readonly URL_MISMATCH_ERR: number;
    readonly QUOTA_EXCEEDED_ERR: number; readonly TIMEOUT_ERR: number; readonly INVALID_NODE_TYPE_ERR: number;
    readonly DATA_CLONE_ERR: number;
  }
  interface MessageEvent<T = unknown> extends Event { readonly data: T; readonly origin: string; readonly lastEventId: string; readonly source: MessagePort | null; readonly ports: readonly MessagePort[] }
  interface MessageEventInit<T = unknown> extends EventInit { data?: T; origin?: string; lastEventId?: string; source?: MessagePort | null; ports?: Iterable<MessagePort> }
  interface MessageEventConstructor { new<T = unknown>(type: string, init?: MessageEventInit<T>): MessageEvent<T> }
  type Transferable = ArrayBuffer | MessagePort;
  interface MessagePort extends EventTarget {
    onmessage: EventListener<MessageEvent> | null;
    onmessageerror: EventListener<MessageEvent> | null;
    on(type: "message" | "messageerror", listener: (data: unknown) => void): this;
    on(type: "close", listener: () => void): this;
    on(type: string, listener: EventCallback): this;
    addListener(type: "message" | "messageerror", listener: (data: unknown) => void): this;
    addListener(type: "close", listener: () => void): this;
    addListener(type: string, listener: EventCallback): this;
    once(type: "message" | "messageerror", listener: (data: unknown) => void): this;
    once(type: "close", listener: () => void): this;
    once(type: string, listener: EventCallback): this;
    off(type: string, listener: EventCallback): this;
    removeListener(type: string, listener: EventCallback): this;
    removeAllListeners(type?: string): this;
    postMessage(message: unknown, transfer?: Iterable<Transferable> | {transfer: Iterable<Transferable>}): void;
    start(): void;
    close(): void;
    ref(): this;
    unref(): this;
    hasRef(): boolean;
  }
  interface MessagePortConstructor {
    readonly prototype: MessagePort;
    [Symbol.hasInstance](value: unknown): value is MessagePort;
  }
  interface MessageChannel { readonly port1: MessagePort; readonly port2: MessagePort }
  interface MessageChannelConstructor { new(): MessageChannel }
  interface BroadcastChannel extends EventTarget {
    readonly name: string;
    onmessage: EventListener<MessageEvent> | null;
    onmessageerror: EventListener<MessageEvent> | null;
    postMessage(message: unknown): void;
    close(): void;
    ref(): this;
    unref(): this;
  }
  interface BroadcastChannelConstructor { new(name: string): BroadcastChannel }
  interface ErrorEvent extends Event { readonly message: string; readonly filename: string; readonly lineno: number; readonly colno: number; readonly error: unknown }
  interface ErrorEventConstructor { new(type: string, init?: {message?: string; filename?: string; lineno?: number; colno?: number; error?: unknown; cancelable?: boolean}): ErrorEvent }

  type HeadersInit = Headers | Iterable<readonly [string, string]> | Readonly<Record<string, string>>;
  type BodyInit = string | ArrayBuffer | ArrayBufferView | Blob | FormData | URLSearchParams | ReadableStream<Uint8Array>;
  interface Headers extends Iterable<[string, string]> {
    append(name: string, value: string): void;
    delete(name: string): void;
    get(name: string): string | null;
    getSetCookie(): string[];
    has(name: string): boolean;
    set(name: string, value: string): void;
    forEach(callback: (value: string, key: string, headers: Headers) => void, thisArg?: unknown): void;
    entries(): IterableIterator<[string, string]>;
    keys(): IterableIterator<string>;
    values(): IterableIterator<string>;
    toJSON(): Record<string, string>;
  }
  interface HeadersConstructor { new(init?: HeadersInit): Headers }
  interface Body {
    readonly body: ReadableStream<Uint8Array> | null;
    readonly bodyUsed: boolean;
    arrayBuffer(): Promise<ArrayBuffer>;
    bytes(): Promise<Uint8Array>;
    blob(): Promise<Blob>;
    formData(): Promise<FormData>;
    json(): Promise<unknown>;
    text(): Promise<string>;
  }
  interface RequestInit { method?: string; headers?: HeadersInit; body?: BodyInit | null; signal?: AbortSignal; redirect?: string; credentials?: string; mode?: string }
  interface Request extends Body {
    readonly url: string;
    readonly method: string;
    readonly headers: Headers;
    readonly signal?: AbortSignal;
    readonly redirect: string;
    readonly credentials: string;
    readonly mode: string;
    clone(): Request;
  }
  interface RequestConstructor { new(input: string | URL | Request, init?: RequestInit): Request }
  interface ResponseInit { status?: number; statusText?: string; headers?: HeadersInit; url?: string }
  interface Response extends Body {
    readonly headers: Headers;
    readonly status: number;
    readonly statusText: string;
    readonly url: string;
    readonly redirected: boolean;
    readonly type: string;
    readonly ok: boolean;
    clone(): Response;
  }
  interface ResponseConstructor {
    new(body?: BodyInit | null, init?: ResponseInit): Response;
    json(data: unknown, init?: ResponseInit): Response;
    error(): Response;
    redirect(url: string | URL, status?: number): Response;
  }
  type RequestInfo = string | URL | Request;
  interface FetchInit extends RequestInit {}
  function fetch(input: RequestInfo, init?: FetchInit): Promise<Response>;
  interface HTTPUpgradeOptions { protocol?: string }
  interface WebSocketUpgrade { socket: WebSocket; response: Response & {readonly upgrade: string; readonly protocol?: string} }
  function upgradeWebSocket(request: Request, options?: HTTPUpgradeOptions): WebSocketUpgrade;

  interface WebSocketMessageEvent extends Event { data: string | ArrayBuffer | Blob; origin: string }
  interface WebSocketCloseEvent extends Event { code: number; reason: string; wasClean: boolean }
  interface WebSocketErrorEvent extends Event { error: unknown; message: string }
  interface WebSocket extends EventTarget {
    readonly url: string;
    readonly readyState: number;
    readonly protocol: string;
    readonly extensions: string;
    readonly bufferedAmount: number;
    binaryType: "arraybuffer" | "blob";
    onopen: EventListener<Event> | null;
    onmessage: EventListener<WebSocketMessageEvent> | null;
    onerror: EventListener<WebSocketErrorEvent> | null;
    onclose: EventListener<WebSocketCloseEvent> | null;
    addEventListener(type: "open", listener: EventListener<Event>, options?: {once?: boolean}): void;
    addEventListener(type: "message", listener: EventListener<WebSocketMessageEvent>, options?: {once?: boolean}): void;
    addEventListener(type: "error", listener: EventListener<WebSocketErrorEvent>, options?: {once?: boolean}): void;
    addEventListener(type: "close", listener: EventListener<WebSocketCloseEvent>, options?: {once?: boolean}): void;
    removeEventListener(type: "open", listener: EventListener<Event>): void;
    removeEventListener(type: "message", listener: EventListener<WebSocketMessageEvent>): void;
    removeEventListener(type: "error", listener: EventListener<WebSocketErrorEvent>): void;
    removeEventListener(type: "close", listener: EventListener<WebSocketCloseEvent>): void;
    send(data: string | ArrayBuffer | ArrayBufferView | Blob): void;
    close(code?: number, reason?: string): void;
  }
  interface WebSocketConstructor {
    new(url: string | null, protocols?: string | readonly string[]): WebSocket;
    readonly CONNECTING: 0; readonly OPEN: 1; readonly CLOSING: 2; readonly CLOSED: 3;
  }

  interface Worker extends EventTarget {
    onmessage: EventListener<MessageEvent> | null;
    onmessageerror: EventListener<MessageEvent> | null;
    onerror: EventListener<ErrorEvent> | null;
    postMessage(message: unknown, transfer?: Iterable<Transferable>): void;
    terminate(): void;
  }
  interface WorkerConstructor { new(url: string | URL, options?: {name?: string; type?: "classic" | "module"}): Worker }
  interface CompressionStream extends TransformStream<Uint8Array, Uint8Array> {}
  interface CompressionStreamConstructor { new(format: CompressionFormat): CompressionStream }
  interface DecompressionStream extends TransformStream<Uint8Array, Uint8Array> {}
  interface DecompressionStreamConstructor { new(format: CompressionFormat): DecompressionStream }

  interface Console {
    log(...values: unknown[]): void; info(...values: unknown[]): void; debug(...values: unknown[]): void;
    dir(value: unknown, options?: unknown): void; warn(...values: unknown[]): void; error(...values: unknown[]): void;
    trace(...values: unknown[]): void; assert(condition: unknown, ...values: unknown[]): void;
    group(...values: unknown[]): void; groupCollapsed(...values: unknown[]): void; groupEnd(): void;
    count(label?: string): void; countReset(label?: string): void; time(label?: string): void;
    timeLog(label?: string, ...values: unknown[]): void; timeEnd(label?: string): void; table(value: unknown): void;
  }
  interface Performance { readonly timeOrigin: number; now(): number; mark(): void; measure(): void; clearMarks(): void; clearMeasures(): void }
}

/* Supported Node-style modules. The alias declarations below re-export each
 * canonical module so the two specifiers cannot drift apart. Only APIs that
 * NOE registers are present; e.g. fs does not define Dirent and child_process
 * does not define spawn. */

declare module "node:assert" {
  export type AssertionError = NOEStdlib.AssertionError;
  export const AssertionError: NOEStdlib.AssertionErrorConstructor;
  export import deepEqual = NOEStdlib.deepEqual;
  export import deepStrictEqual = NOEStdlib.deepStrictEqual;
  export import doesNotMatch = NOEStdlib.doesNotMatch;
  export import doesNotReject = NOEStdlib.doesNotReject;
  export import doesNotThrow = NOEStdlib.doesNotThrow;
  export import equal = NOEStdlib.equal;
  export import fail = NOEStdlib.fail;
  export import match = NOEStdlib.match;
  export import notDeepStrictEqual = NOEStdlib.notDeepStrictEqual;
  export import notEqual = NOEStdlib.notEqual;
  export import notStrictEqual = NOEStdlib.notStrictEqual;
  export import ok = NOEStdlib.assert;
  export import rejects = NOEStdlib.rejects;
  const api: NOEStdlib.AssertionAPI;
  export const strict: NOEStdlib.AssertionAPI;
  export import strictEqual = NOEStdlib.strictEqual;
  export import throws = NOEStdlib.throws;
  export default api;
}
declare module "node:async_hooks" {
  export type AsyncLocalStorage<T = unknown> = NOEStdlib.AsyncLocalStorage<T>;
  export const AsyncLocalStorage: NOEStdlib.AsyncLocalStorageConstructor;
  export type AsyncResource = NOEStdlib.AsyncResource;
  export const AsyncResource: NOEStdlib.AsyncResourceConstructor;
  export import createHook = NOEStdlib.createHook;
  export import executionAsyncId = NOEStdlib.executionAsyncId;
  export import executionAsyncResource = NOEStdlib.executionAsyncResource;
  export import triggerAsyncId = NOEStdlib.triggerAsyncId;
  const api: {
    AsyncLocalStorage: NOEStdlib.AsyncLocalStorageConstructor;
    AsyncResource: NOEStdlib.AsyncResourceConstructor;
    createHook: typeof NOEStdlib.createHook;
    executionAsyncId: typeof NOEStdlib.executionAsyncId;
    executionAsyncResource: typeof NOEStdlib.executionAsyncResource;
    triggerAsyncId: typeof NOEStdlib.triggerAsyncId;
  };
  export default api;
}
declare module "node:buffer" {
  export type Buffer = NOEStdlib.Buffer;
  export const Buffer: NOEStdlib.BufferConstructor;
  export const atob: typeof globalThis.atob;
  export const btoa: typeof globalThis.btoa;
  const api: {Buffer: NOEStdlib.BufferConstructor};
  export default api;
}
declare module "node:child_process" {
  export interface ExecOptions extends NOEStdlib.ExecOptions {}
  export type ExecResult<Output = string> = NOEStdlib.ExecResult<Output>;
  export type SpawnSyncResult<Output = string> = NOEStdlib.SpawnSyncResult<Output>;
  export import exec = NOEStdlib.exec;
  export import execFile = NOEStdlib.execFile;
  export import execFileSync = NOEStdlib.execFileSync;
  export import execSync = NOEStdlib.execSync;
  export import spawnSync = NOEStdlib.spawnSync;
  const api: {
    exec: typeof NOEStdlib.exec; execFile: typeof NOEStdlib.execFile;
    execFileSync: typeof NOEStdlib.execFileSync; execSync: typeof NOEStdlib.execSync;
    spawnSync: typeof NOEStdlib.spawnSync;
  };
  export default api;
}
declare module "node:crypto" {
  export type Hash = NOEStdlib.Hash;
  export type CryptoKey = NOEStdlib.CryptoKey;
  export type CryptoKeyUsage = NOEStdlib.CryptoKeyUsage;
  export const constants: Readonly<Record<string, never>>;
  export import createHash = NOEStdlib.createHash;
  export import createHmac = NOEStdlib.createHmac;
  export import getHashes = NOEStdlib.getHashes;
  export const getRandomValues: NOEStdlib.Crypto["getRandomValues"];
  export import hkdf = NOEStdlib.hkdf;
  export import hkdfSync = NOEStdlib.hkdfSync;
  export import pbkdf2 = NOEStdlib.pbkdf2;
  export import pbkdf2Sync = NOEStdlib.pbkdf2Sync;
  export import randomBytes = NOEStdlib.randomBytes;
  export import randomFillSync = NOEStdlib.randomFillSync;
  export import randomInt = NOEStdlib.randomInt;
  export const randomUUID: NOEStdlib.Crypto["randomUUID"];
  export const subtle: NOEStdlib.WebCryptoSubtle;
  export import timingSafeEqual = NOEStdlib.timingSafeEqual;
  export const webcrypto: NOEStdlib.Crypto;
  const api: {
    constants: Readonly<Record<string, never>>;
    createHash: typeof NOEStdlib.createHash; createHmac: typeof NOEStdlib.createHmac;
    getHashes: typeof NOEStdlib.getHashes; getRandomValues: NOEStdlib.Crypto["getRandomValues"];
    hkdf: typeof NOEStdlib.hkdf; hkdfSync: typeof NOEStdlib.hkdfSync;
    pbkdf2: typeof NOEStdlib.pbkdf2; pbkdf2Sync: typeof NOEStdlib.pbkdf2Sync;
    randomBytes: typeof NOEStdlib.randomBytes; randomFillSync: typeof NOEStdlib.randomFillSync;
    randomInt: typeof NOEStdlib.randomInt; randomUUID: NOEStdlib.Crypto["randomUUID"];
    subtle: NOEStdlib.WebCryptoSubtle; timingSafeEqual: typeof NOEStdlib.timingSafeEqual;
    webcrypto: NOEStdlib.Crypto;
  };
  export default api;
}
declare module "node:dns" {
  export type DNSAddress = NOEStdlib.DNSAddress; export type DNSMX = NOEStdlib.DNSMX; export type DNSSRV = NOEStdlib.DNSSRV;
  export type DNSAnswer = NOEStdlib.DNSAnswer; export type DNSFailure = NOEStdlib.DNSFailure;
  export interface Resolver extends NOEStdlib.DNSCallbackResolver {}
  export const Resolver: {new(options?: object): Resolver};
  export const getDefaultResultOrder: NOEStdlib.DNSCallbackAPI["getDefaultResultOrder"];
  export const getServers: NOEStdlib.DNSCallbackAPI["getServers"];
  export const lookup: NOEStdlib.DNSCallbackAPI["lookup"];
  export const lookupService: NOEStdlib.DNSCallbackAPI["lookupService"];
  export const resolve: NOEStdlib.DNSCallbackAPI["resolve"];
  export const resolve4: NOEStdlib.DNSCallbackAPI["resolve4"];
  export const resolve6: NOEStdlib.DNSCallbackAPI["resolve6"];
  export const resolveAny: NOEStdlib.DNSCallbackAPI["resolveAny"];
  export const resolveCaa: NOEStdlib.DNSCallbackAPI["resolveCaa"];
  export const resolveCname: NOEStdlib.DNSCallbackAPI["resolveCname"];
  export const resolveMx: NOEStdlib.DNSCallbackAPI["resolveMx"];
  export const resolveNaptr: NOEStdlib.DNSCallbackAPI["resolveNaptr"];
  export const resolveNs: NOEStdlib.DNSCallbackAPI["resolveNs"];
  export const resolvePtr: NOEStdlib.DNSCallbackAPI["resolvePtr"];
  export const resolveSoa: NOEStdlib.DNSCallbackAPI["resolveSoa"];
  export const resolveSrv: NOEStdlib.DNSCallbackAPI["resolveSrv"];
  export const resolveTlsa: NOEStdlib.DNSCallbackAPI["resolveTlsa"];
  export const resolveTxt: NOEStdlib.DNSCallbackAPI["resolveTxt"];
  export const reverse: NOEStdlib.DNSCallbackAPI["reverse"];
  export const setDefaultResultOrder: NOEStdlib.DNSCallbackAPI["setDefaultResultOrder"];
  export const setServers: NOEStdlib.DNSCallbackAPI["setServers"];
  export const promises: NOEStdlib.DNSPromisesAPI;
  export const ADDRCONFIG: number; export const ADDRGETNETWORKPARAMS: number; export const ALL: number;
  export const BADFAMILY: number; export const BADFLAGS: number; export const BADHINTS: number;
  export const BADNAME: number; export const BADQUERY: number; export const BADRESP: number; export const BADSTR: number;
  export const CANCELLED: number; export const CONNREFUSED: number; export const DESTRUCTION: number; export const EOF: number;
  export const FILE: number; export const FORMERR: number; export const LOADIPHLPAPI: number; export const NODATA: number;
  export const NOMEM: number; export const NONAME: number; export const NOTFOUND: number; export const NOTIMP: number;
  export const NOTINITIALIZED: number; export const REFUSED: number; export const SERVFAIL: number; export const TIMEOUT: number;
  export const V4MAPPED: number;
  const api: NOEStdlib.DNSCallbackAPI;
  export default api;
}
declare module "node:dns/promises" {
  export type DNSAddress = NOEStdlib.DNSAddress; export type DNSMX = NOEStdlib.DNSMX; export type DNSSRV = NOEStdlib.DNSSRV;
  export type DNSAnswer = NOEStdlib.DNSAnswer; export type DNSFailure = NOEStdlib.DNSFailure;
  export type Resolver = NOEStdlib.PromiseResolver;
  export const Resolver: NOEStdlib.PromiseResolverConstructor;
  export const getDefaultResultOrder: NOEStdlib.DNSPromisesAPI["getDefaultResultOrder"];
  export const getServers: NOEStdlib.DNSPromisesAPI["getServers"];
  export const lookup: (hostname: string, options?: NOEStdlib.DNSFamily | NOEStdlib.DNSLookupOptions) => Promise<NOEStdlib.DNSLookupResult | NOEStdlib.DNSAddress[]>;
  export const lookupService: (address: string, port: number) => Promise<NOEStdlib.DNSService>;
  export const resolve: (hostname: string, type?: NOEStdlib.DNSRecordType) => Promise<NOEStdlib.DNSAnswer>;
  export const resolve4: NOEStdlib.PromiseResolver["resolve4"];
  export const resolve6: NOEStdlib.PromiseResolver["resolve6"];
  export const resolveAny: (hostname: string, options?: NOEStdlib.DNSResolveOptions) => Promise<never>;
  export const resolveCaa: (hostname: string, options?: NOEStdlib.DNSResolveOptions) => Promise<never>;
  export const resolveCname: (hostname: string, options?: NOEStdlib.DNSResolveOptions) => Promise<string[]>;
  export const resolveMx: (hostname: string, options?: NOEStdlib.DNSResolveOptions) => Promise<NOEStdlib.DNSMX[]>;
  export const resolveNaptr: (hostname: string, options?: NOEStdlib.DNSResolveOptions) => Promise<never>;
  export const resolveNs: (hostname: string, options?: NOEStdlib.DNSResolveOptions) => Promise<string[]>;
  export const resolvePtr: (hostname: string, options?: NOEStdlib.DNSResolveOptions) => Promise<string[]>;
  export const resolveSoa: (hostname: string, options?: NOEStdlib.DNSResolveOptions) => Promise<never>;
  export const resolveSrv: (hostname: string, options?: NOEStdlib.DNSResolveOptions) => Promise<NOEStdlib.DNSSRV[]>;
  export const resolveTlsa: (hostname: string, options?: NOEStdlib.DNSResolveOptions) => Promise<never>;
  export const resolveTxt: (hostname: string, options?: NOEStdlib.DNSResolveOptions) => Promise<string[][]>;
  export const reverse: (address: string) => Promise<string[]>;
  export const setDefaultResultOrder: NOEStdlib.DNSPromisesAPI["setDefaultResultOrder"];
  export const setServers: NOEStdlib.DNSPromisesAPI["setServers"];
  export const ADDRGETNETWORKPARAMS: number; export const BADFAMILY: number; export const BADFLAGS: number;
  export const BADHINTS: number; export const BADNAME: number; export const BADQUERY: number; export const BADRESP: number; export const BADSTR: number;
  export const CANCELLED: number; export const CONNREFUSED: number; export const DESTRUCTION: number; export const EOF: number;
  export const FILE: number; export const FORMERR: number; export const LOADIPHLPAPI: number; export const NODATA: number;
  export const NOMEM: number; export const NONAME: number; export const NOTFOUND: number; export const NOTIMP: number;
  export const NOTINITIALIZED: number; export const REFUSED: number; export const SERVFAIL: number; export const TIMEOUT: number;
  const api: NOEStdlib.DNSPromisesAPI;
  export default api;
}
declare module "node:events" {
  export type EventEmitter = NOEStdlib.EventEmitter;
  export const EventEmitter: NOEStdlib.EventEmitterConstructor;
  export import once = NOEStdlib.once;
  export default EventEmitter;
}
declare module "node:fs" {
  export type FileInfo = NOEStdlib.FileInfo; export type FileData = NOEStdlib.FileData;
  export type FileReadOptions = NOEStdlib.FileReadOptions; export type FileWriteOptions = NOEStdlib.FileWriteOptions;
  export type FSPromisesAPI = NOEStdlib.FSPromisesAPI;
  export const appendFileSync: NOEStdlib.FSModule["appendFileSync"];
  export const chmodSync: NOEStdlib.FSModule["chmodSync"];
  export const copyFileSync: NOEStdlib.FSModule["copyFileSync"];
  export const createReadStream: NOEStdlib.FSModule["createReadStream"];
  export const createWriteStream: NOEStdlib.FSModule["createWriteStream"];
  export const existsSync: NOEStdlib.FSModule["existsSync"];
  export const lstatSync: NOEStdlib.FSModule["lstatSync"];
  export const mkdirSync: NOEStdlib.FSModule["mkdirSync"];
  export const mkdtempSync: NOEStdlib.FSModule["mkdtempSync"];
  export const readFileSync: NOEStdlib.FSModule["readFileSync"];
  export const readdirSync: NOEStdlib.FSModule["readdirSync"];
  export const readlinkSync: NOEStdlib.FSModule["readlinkSync"];
  export const realpathSync: NOEStdlib.FSModule["realpathSync"];
  export const renameSync: NOEStdlib.FSModule["renameSync"];
  export const rmSync: NOEStdlib.FSModule["rmSync"];
  export const rmdirSync: NOEStdlib.FSModule["rmdirSync"];
  export const statSync: NOEStdlib.FSModule["statSync"];
  export const unlinkSync: NOEStdlib.FSModule["unlinkSync"];
  export const writeFileSync: NOEStdlib.FSModule["writeFileSync"];
  export const promises: NOEStdlib.FSPromisesAPI;
  const api: NOEStdlib.FSModule;
  export default api;
}
declare module "node:fs/promises" {
  export type FileInfo = NOEStdlib.FileInfo; export type FileData = NOEStdlib.FileData;
  export const appendFile: NOEStdlib.FSPromisesAPI["appendFile"];
  export const copyFile: NOEStdlib.FSPromisesAPI["copyFile"];
  export const lstat: NOEStdlib.FSPromisesAPI["lstat"];
  export const mkdir: NOEStdlib.FSPromisesAPI["mkdir"];
  export const mkdtemp: NOEStdlib.FSPromisesAPI["mkdtemp"];
  export const readFile: NOEStdlib.FSPromisesAPI["readFile"];
  export const readdir: NOEStdlib.FSPromisesAPI["readdir"];
  export const readlink: NOEStdlib.FSPromisesAPI["readlink"];
  export const realpath: NOEStdlib.FSPromisesAPI["realpath"];
  export const rename: NOEStdlib.FSPromisesAPI["rename"];
  export const rm: NOEStdlib.FSPromisesAPI["rm"];
  export const rmdir: NOEStdlib.FSPromisesAPI["rmdir"];
  export const stat: NOEStdlib.FSPromisesAPI["stat"];
  export const unlink: NOEStdlib.FSPromisesAPI["unlink"];
  export const writeFile: NOEStdlib.FSPromisesAPI["writeFile"];
  const api: NOEStdlib.FSPromisesAPI;
  export default api;
}
declare module "node:http" {
  export type HTTPServer = NOEStdlib.HTTPServer; export type ServeOptions = NOEStdlib.ServeOptions;
  export import serve = NOEStdlib.serve;
  const api: {serve: typeof NOEStdlib.serve};
  export default api;
}
declare module "node:os" {
  export const arch: NOEStdlib.OSModule["arch"];
  export const cpus: NOEStdlib.OSModule["cpus"];
  export const homedir: NOEStdlib.OSModule["homedir"];
  export const hostname: NOEStdlib.OSModule["hostname"];
  export const platform: NOEStdlib.OSModule["platform"];
  export const tmpdir: NOEStdlib.OSModule["tmpdir"];
  export const type: NOEStdlib.OSModule["type"];
  export const EOL: string;
  const api: NOEStdlib.OSModule;
  export default api;
}
declare module "node:path" {
  export type PathAPI = NOEStdlib.PathAPI; export type PathObject = NOEStdlib.PathObject; export type PathFormatObject = NOEStdlib.PathFormatObject;
  export const basename: NOEStdlib.PathAPI["basename"];
  export const dirname: NOEStdlib.PathAPI["dirname"];
  export const extname: NOEStdlib.PathAPI["extname"];
  export const format: NOEStdlib.PathAPI["format"];
  export const isAbsolute: NOEStdlib.PathAPI["isAbsolute"];
  export const join: NOEStdlib.PathAPI["join"];
  export const normalize: NOEStdlib.PathAPI["normalize"];
  export const parse: NOEStdlib.PathAPI["parse"];
  export const relative: NOEStdlib.PathAPI["relative"];
  export const resolve: NOEStdlib.PathAPI["resolve"];
  export const toNamespacedPath: NOEStdlib.PathAPI["toNamespacedPath"];
  export const _makeLong: NOEStdlib.PathAPI["_makeLong"];
  export const delimiter: string; export const sep: string;
  export const posix: NOEStdlib.PathAPI; export const win32: NOEStdlib.PathAPI;
  const api: NOEStdlib.PathAPI;
  export default api;
}
declare module "node:path/posix" {
  export type PathAPI = NOEStdlib.PathAPI;
  export const basename: NOEStdlib.PathAPI["basename"]; export const dirname: NOEStdlib.PathAPI["dirname"];
  export const extname: NOEStdlib.PathAPI["extname"]; export const format: NOEStdlib.PathAPI["format"];
  export const isAbsolute: NOEStdlib.PathAPI["isAbsolute"]; export const join: NOEStdlib.PathAPI["join"];
  export const normalize: NOEStdlib.PathAPI["normalize"]; export const parse: NOEStdlib.PathAPI["parse"];
  export const relative: NOEStdlib.PathAPI["relative"]; export const resolve: NOEStdlib.PathAPI["resolve"];
  export const toNamespacedPath: NOEStdlib.PathAPI["toNamespacedPath"]; export const _makeLong: NOEStdlib.PathAPI["_makeLong"];
  export const delimiter: string; export const sep: string; export const posix: NOEStdlib.PathAPI; export const win32: NOEStdlib.PathAPI;
  const api: NOEStdlib.PathAPI; export default api;
}
declare module "node:path/win32" {
  export type PathAPI = NOEStdlib.PathAPI;
  export const basename: NOEStdlib.PathAPI["basename"]; export const dirname: NOEStdlib.PathAPI["dirname"];
  export const extname: NOEStdlib.PathAPI["extname"]; export const format: NOEStdlib.PathAPI["format"];
  export const isAbsolute: NOEStdlib.PathAPI["isAbsolute"]; export const join: NOEStdlib.PathAPI["join"];
  export const normalize: NOEStdlib.PathAPI["normalize"]; export const parse: NOEStdlib.PathAPI["parse"];
  export const relative: NOEStdlib.PathAPI["relative"]; export const resolve: NOEStdlib.PathAPI["resolve"];
  export const toNamespacedPath: NOEStdlib.PathAPI["toNamespacedPath"]; export const _makeLong: NOEStdlib.PathAPI["_makeLong"];
  export const delimiter: string; export const sep: string; export const posix: NOEStdlib.PathAPI; export const win32: NOEStdlib.PathAPI;
  const api: NOEStdlib.PathAPI; export default api;
}
declare module "node:process" {
  export type Process = NOEStdlib.Process;
  const process: NOEStdlib.Process;
  export default process;
}
declare module "node:querystring" {
  export type ParsedUrlQuery = NOEStdlib.QueryStringRecord;
  export import decode = NOEStdlib.parseQuery; export import parse = NOEStdlib.parseQuery;
  export import encode = NOEStdlib.stringifyQuery; export import stringify = NOEStdlib.stringifyQuery;
  export function escape(value: unknown): string;
  export function unescape(value: string): string;
  const api: NOEStdlib.QueryStringAPI; export default api;
}
declare module "node:stream/web" {
  export type ReadableStream<T = unknown> = NOEStdlib.ReadableStream<T>; export const ReadableStream: NOEStdlib.ReadableStreamConstructor;
  export type ReadableStreamDefaultController<T = unknown> = NOEStdlib.ReadableStreamDefaultController<T>; export const ReadableStreamDefaultController: NOEStdlib.ReadableStreamDefaultControllerConstructor;
  export type ReadableStreamDefaultReader<T = unknown> = NOEStdlib.ReadableStreamDefaultReader<T>; export const ReadableStreamDefaultReader: NOEStdlib.ReadableStreamDefaultReaderConstructor;
  export type WritableStream<T = unknown> = NOEStdlib.WritableStream<T>; export const WritableStream: NOEStdlib.WritableStreamConstructor;
  export type WritableStreamDefaultWriter<T = unknown> = NOEStdlib.WritableStreamDefaultWriter<T>; export const WritableStreamDefaultWriter: NOEStdlib.WritableStreamDefaultWriterConstructor;
  export type TransformStream<I = unknown, O = unknown> = NOEStdlib.TransformStream<I, O>; export const TransformStream: NOEStdlib.TransformStreamConstructor;
  export type ByteLengthQueuingStrategy = NOEStdlib.QueuingStrategyObject<Uint8Array>; export const ByteLengthQueuingStrategy: NOEStdlib.QueuingStrategyConstructor<Uint8Array>;
  export type CountQueuingStrategy = NOEStdlib.QueuingStrategyObject<unknown>; export const CountQueuingStrategy: NOEStdlib.QueuingStrategyConstructor<unknown>;
  export type TextEncoderStream = NOEStdlib.TextEncoderStream; export const TextEncoderStream: NOEStdlib.TextEncoderStreamConstructor;
  export type TextDecoderStream = NOEStdlib.TextDecoderStream; export const TextDecoderStream: NOEStdlib.TextDecoderStreamConstructor;
  const api: {
    ReadableStream: NOEStdlib.ReadableStreamConstructor; ReadableStreamDefaultController: NOEStdlib.ReadableStreamDefaultControllerConstructor;
    ReadableStreamDefaultReader: NOEStdlib.ReadableStreamDefaultReaderConstructor; WritableStream: NOEStdlib.WritableStreamConstructor;
    WritableStreamDefaultWriter: NOEStdlib.WritableStreamDefaultWriterConstructor; TransformStream: NOEStdlib.TransformStreamConstructor;
    ByteLengthQueuingStrategy: NOEStdlib.QueuingStrategyConstructor<Uint8Array>; CountQueuingStrategy: NOEStdlib.QueuingStrategyConstructor<unknown>;
    TextEncoderStream: NOEStdlib.TextEncoderStreamConstructor; TextDecoderStream: NOEStdlib.TextDecoderStreamConstructor;
  };
  export default api;
}
declare module "node:string_decoder" {
  export type StringDecoder = NOEStdlib.StringDecoder;
  export const StringDecoder: NOEStdlib.StringDecoderConstructor;
  const api: {StringDecoder: NOEStdlib.StringDecoderConstructor}; export default api;
}
declare module "node:timers" {
  export const clearImmediate: NOEStdlib.TimersAPI["clearImmediate"];
  export const clearInterval: NOEStdlib.TimersAPI["clearInterval"];
  export const clearTimeout: NOEStdlib.TimersAPI["clearTimeout"];
  export const setImmediate: NOEStdlib.TimersAPI["setImmediate"];
  export const setInterval: NOEStdlib.TimersAPI["setInterval"];
  export const setTimeout: NOEStdlib.TimersAPI["setTimeout"];
  const api: NOEStdlib.TimersAPI; export default api;
}
declare module "node:timers/promises" {
  export const setImmediate: NOEStdlib.TimerPromisesAPI["setImmediate"];
  export const setInterval: NOEStdlib.TimerPromisesAPI["setInterval"];
  export const setTimeout: NOEStdlib.TimerPromisesAPI["setTimeout"];
  export const scheduler: NOEStdlib.TimerPromisesAPI["scheduler"];
  const api: NOEStdlib.TimerPromisesAPI; export default api;
}
declare module "node:url" {
  export type URL = NOEStdlib.URL; export const URL: NOEStdlib.URLConstructor;
  export type URLSearchParams = NOEStdlib.URLSearchParams; export const URLSearchParams: NOEStdlib.URLSearchParamsConstructor;
  export const domainToASCII: NOEStdlib.URLModule["domainToASCII"];
  export const domainToUnicode: NOEStdlib.URLModule["domainToUnicode"];
  export const fileURLToPath: NOEStdlib.URLModule["fileURLToPath"];
  export const format: NOEStdlib.URLModule["format"];
  export const parse: NOEStdlib.URLModule["parse"];
  export const pathToFileURL: NOEStdlib.URLModule["pathToFileURL"];
  export const resolve: NOEStdlib.URLModule["resolve"];
  const api: NOEStdlib.URLModule; export default api;
}
declare module "node:util" {
  export import callbackify = NOEStdlib.callbackify;
  export import deprecate = NOEStdlib.deprecate;
  export import format = NOEStdlib.format;
  export import inherits = NOEStdlib.inherits;
  export import inspect = NOEStdlib.inspect;
  export import isDeepStrictEqual = NOEStdlib.isDeepStrictEqual;
  export import promisify = NOEStdlib.promisify;
  export const types: NOEStdlib.UtilTypes;
  const api: {
    callbackify: typeof NOEStdlib.callbackify; deprecate: typeof NOEStdlib.deprecate; format: typeof NOEStdlib.format;
    inherits: typeof NOEStdlib.inherits; inspect: typeof NOEStdlib.inspect; isDeepStrictEqual: typeof NOEStdlib.isDeepStrictEqual;
    promisify: typeof NOEStdlib.promisify; types: NOEStdlib.UtilTypes;
  };
  export default api;
}
declare module "node:vm" {
  export type Script = NOEStdlib.Script; export const Script: NOEStdlib.ScriptConstructor;
  export const compileFunction: NOEStdlib.VMModule["compileFunction"];
  export const constants: NOEStdlib.VMConstants;
  export const createContext: NOEStdlib.VMModule["createContext"];
  export const createScript: NOEStdlib.VMModule["createScript"];
  export const isContext: NOEStdlib.VMModule["isContext"];
  export const measureMemory: NOEStdlib.VMModule["measureMemory"];
  export const runInContext: NOEStdlib.VMModule["runInContext"];
  export const runInNewContext: NOEStdlib.VMModule["runInNewContext"];
  export const runInThisContext: NOEStdlib.VMModule["runInThisContext"];
  const api: NOEStdlib.VMModule; export default api;
}
declare module "node:worker_threads" {
  export type Worker = NOEStdlib.NodeWorker; export const Worker: NOEStdlib.NodeWorkerConstructor;
  export const BroadcastChannel: NOEStdlib.BroadcastChannelConstructor;
  export const MessageChannel: NOEStdlib.MessageChannelConstructor;
  export const MessagePort: NOEStdlib.MessagePortConstructor;
  export const SHARE_ENV: typeof NOEStdlib.SHARE_ENV;
  export const getEnvironmentData: NOEStdlib.WorkerThreadsAPI["getEnvironmentData"];
  export const isMainThread: boolean;
  export const isMarkedAsUntransferable: NOEStdlib.WorkerThreadsAPI["isMarkedAsUntransferable"];
  export const markAsUntransferable: NOEStdlib.WorkerThreadsAPI["markAsUntransferable"];
  export const parentPort: NOEStdlib.MessagePort | null;
  export const receiveMessageOnPort: NOEStdlib.WorkerThreadsAPI["receiveMessageOnPort"];
  export const resourceLimits: Readonly<Record<string, never>>;
  export const setEnvironmentData: NOEStdlib.WorkerThreadsAPI["setEnvironmentData"];
  export const threadId: number;
  export const workerData: unknown;
  const api: NOEStdlib.WorkerThreadsAPI; export default api;
}
declare module "node:zlib" {
  export type ZlibAPI = NOEStdlib.ZlibAPI;
  export const constants: NOEStdlib.ZlibAPI["constants"];
  export const createDeflate: NOEStdlib.ZlibAPI["createDeflate"];
  export const createDeflateRaw: NOEStdlib.ZlibAPI["createDeflateRaw"];
  export const createGunzip: NOEStdlib.ZlibAPI["createGunzip"];
  export const createGzip: NOEStdlib.ZlibAPI["createGzip"];
  export const createInflate: NOEStdlib.ZlibAPI["createInflate"];
  export const createInflateRaw: NOEStdlib.ZlibAPI["createInflateRaw"];
  export const deflate: NOEStdlib.ZlibAsyncFunction; export const deflateRaw: NOEStdlib.ZlibAsyncFunction;
  export const deflateRawSync: NOEStdlib.ZlibSyncFunction; export const deflateSync: NOEStdlib.ZlibSyncFunction;
  export const gunzip: NOEStdlib.ZlibAsyncFunction; export const gunzipSync: NOEStdlib.ZlibSyncFunction;
  export const gzip: NOEStdlib.ZlibAsyncFunction; export const gzipSync: NOEStdlib.ZlibSyncFunction;
  export const inflate: NOEStdlib.ZlibAsyncFunction; export const inflateRaw: NOEStdlib.ZlibAsyncFunction;
  export const inflateRawSync: NOEStdlib.ZlibSyncFunction; export const inflateSync: NOEStdlib.ZlibSyncFunction;
  export const unzip: NOEStdlib.ZlibAPI["unzip"];
  export const unzipSync: NOEStdlib.ZlibAPI["unzipSync"];
  const api: NOEStdlib.ZlibAPI; export default api;
}

/* The 26 reserved bare specifiers are aliases for their node:-prefixed forms. */
declare module "assert" { export * from "node:assert"; export {default} from "node:assert"; }
declare module "async_hooks" { export * from "node:async_hooks"; export {default} from "node:async_hooks"; }
declare module "buffer" { export * from "node:buffer"; export {default} from "node:buffer"; }
declare module "child_process" { export * from "node:child_process"; export {default} from "node:child_process"; }
declare module "crypto" { export * from "node:crypto"; export {default} from "node:crypto"; }
declare module "dns" { export * from "node:dns"; export {default} from "node:dns"; }
declare module "dns/promises" { export * from "node:dns/promises"; export {default} from "node:dns/promises"; }
declare module "events" { export * from "node:events"; export {default} from "node:events"; }
declare module "fs" { export * from "node:fs"; export {default} from "node:fs"; }
declare module "fs/promises" { export * from "node:fs/promises"; export {default} from "node:fs/promises"; }
declare module "http" { export * from "node:http"; export {default} from "node:http"; }
declare module "os" { export * from "node:os"; export {default} from "node:os"; }
declare module "path" { export * from "node:path"; export {default} from "node:path"; }
declare module "path/posix" { export * from "node:path/posix"; export {default} from "node:path/posix"; }
declare module "path/win32" { export * from "node:path/win32"; export {default} from "node:path/win32"; }
declare module "process" { export * from "node:process"; export {default} from "node:process"; }
declare module "querystring" { export * from "node:querystring"; export {default} from "node:querystring"; }
declare module "stream/web" { export * from "node:stream/web"; export {default} from "node:stream/web"; }
declare module "string_decoder" { export * from "node:string_decoder"; export {default} from "node:string_decoder"; }
declare module "timers" { export * from "node:timers"; export {default} from "node:timers"; }
declare module "timers/promises" { export * from "node:timers/promises"; export {default} from "node:timers/promises"; }
declare module "url" { export * from "node:url"; export {default} from "node:url"; }
declare module "util" { export * from "node:util"; export {default} from "node:util"; }
declare module "vm" { export * from "node:vm"; export {default} from "node:vm"; }
declare module "worker_threads" { export * from "node:worker_threads"; export {default} from "node:worker_threads"; }
declare module "zlib" { export * from "node:zlib"; export {default} from "node:zlib"; }

declare module "noe:docs" { export import docs = NOEStdlib.docs; }
declare module "noe:io" {
  export const stdin: ReadableStream<Uint8Array>;
  export const stdout: WritableStream<Uint8Array>;
  export const stderr: WritableStream<Uint8Array>;
  export import pipe = NOEStdlib.pipe;
}
declare module "noe:process" { export import spawn = NOEStdlib.spawn; export type ChildProcess = NOEStdlib.OwnedChildProcess; export type SpawnOptions = NOEStdlib.SpawnOptions; }
declare module "noe:http" {
  export import fetch = NOEStdlib.fetch;
  export import serve = NOEStdlib.serve;
  export type FetchInit = NOEStdlib.FetchInit;
  export type HTTPServer = NOEStdlib.HTTPServer;
  export type ServeOptions = NOEStdlib.ServeOptions;
}
declare module "noe:fs-handle" { export import open = NOEStdlib.open; export type FileHandle = NOEStdlib.FileHandle; export type FileHandleRead = NOEStdlib.FileHandleRead; export type FileHandleMode = NOEStdlib.FileHandleMode; }
declare module "noe:sockets" {
  export import connect = NOEStdlib.connect; export import listen = NOEStdlib.listen;
  export type Connection = NOEStdlib.RawConnection; export type Listener = NOEStdlib.SocketListener;
  export type Address = NOEStdlib.SocketAddress; export type ConnectOptions = NOEStdlib.ConnectOptions; export type ListenOptions = NOEStdlib.ListenOptions;
}
declare module "noe:signals" { export import subscribe = NOEStdlib.subscribe; export type SignalName = NOEStdlib.SignalName; export type SignalSubscription = NOEStdlib.SignalSubscription; }

/* NOE Web and Node-compatible globals. These declarations expose the
 * implemented subset; Profile and Worker selection determine runtime presence. */
type CompressionFormat = NOEStdlib.CompressionFormat;
type Transferable = NOEStdlib.Transferable;
type BlobPart = NOEStdlib.BlobPart;
type FormDataEntryValue = NOEStdlib.FormDataEntryValue;
type HeadersInit = NOEStdlib.HeadersInit;
type BodyInit = NOEStdlib.BodyInit;
type RequestInfo = NOEStdlib.RequestInfo;
interface RequestInit extends NOEStdlib.RequestInit {}
interface ResponseInit extends NOEStdlib.ResponseInit {}
interface EventInit extends NOEStdlib.EventInit {}
type EventListener<E extends Event = Event> = NOEStdlib.EventListener<E>;
interface EventListenerObject<E extends Event = Event> extends NOEStdlib.EventListenerObject<E> {}
type EventListenerOrEventListenerObject<E extends Event = Event> = NOEStdlib.EventListenerOrObject<E>;
type ReadableStreamReadResult<T> = NOEStdlib.ReadableStreamReadResult<T>;
type ReadableStreamReadDoneResult = NOEStdlib.ReadableStreamReadDoneResult;
type ReadableStreamReadValueResult<T> = NOEStdlib.ReadableStreamReadValueResult<T>;
interface URL extends NOEStdlib.URL {}
declare var URL: NOEStdlib.URLConstructor;
interface URLSearchParams extends NOEStdlib.URLSearchParams {}
declare var URLSearchParams: NOEStdlib.URLSearchParamsConstructor;
interface TextEncoder extends NOEStdlib.TextEncoder {}
declare var TextEncoder: NOEStdlib.TextEncoderConstructor;
interface TextDecoder extends NOEStdlib.TextDecoder {}
declare var TextDecoder: NOEStdlib.TextDecoderConstructor;
interface Blob extends NOEStdlib.Blob {}
declare var Blob: NOEStdlib.BlobConstructor;
interface File extends NOEStdlib.File {}
declare var File: NOEStdlib.FileConstructor;
interface FormData extends NOEStdlib.FormData {}
declare var FormData: NOEStdlib.FormDataConstructor;
interface URLPattern extends NOEStdlib.URLPattern {}
declare var URLPattern: NOEStdlib.URLPatternConstructor;
interface Event extends NOEStdlib.Event {}
declare var Event: NOEStdlib.EventConstructor;
interface EventTarget extends NOEStdlib.EventTarget {}
declare var EventTarget: NOEStdlib.EventTargetConstructor;
interface AbortSignal extends NOEStdlib.AbortSignal {}
declare var AbortSignal: NOEStdlib.AbortSignalConstructor;
interface AbortController extends NOEStdlib.AbortController {}
declare var AbortController: NOEStdlib.AbortControllerConstructor;
interface DOMException extends NOEStdlib.DOMException {}
declare var DOMException: NOEStdlib.DOMExceptionConstructor;
interface MessageEvent<T = unknown> extends NOEStdlib.MessageEvent<T> {}
declare var MessageEvent: NOEStdlib.MessageEventConstructor;
interface MessagePort extends NOEStdlib.MessagePort {}
declare var MessagePort: NOEStdlib.MessagePortConstructor;
interface MessageChannel extends NOEStdlib.MessageChannel {}
declare var MessageChannel: NOEStdlib.MessageChannelConstructor;
interface BroadcastChannel extends NOEStdlib.BroadcastChannel {}
declare var BroadcastChannel: NOEStdlib.BroadcastChannelConstructor;
interface ErrorEvent extends NOEStdlib.ErrorEvent {}
declare var ErrorEvent: NOEStdlib.ErrorEventConstructor;
interface Headers extends NOEStdlib.Headers {}
declare var Headers: NOEStdlib.HeadersConstructor;
interface Request extends NOEStdlib.Request {}
declare var Request: NOEStdlib.RequestConstructor;
interface Response extends NOEStdlib.Response {}
declare var Response: NOEStdlib.ResponseConstructor;
interface ReadableStream<T = unknown> extends NOEStdlib.ReadableStream<T> {}
declare var ReadableStream: NOEStdlib.ReadableStreamConstructor;
interface ReadableStreamDefaultController<T = unknown> extends NOEStdlib.ReadableStreamDefaultController<T> {}
declare var ReadableStreamDefaultController: NOEStdlib.ReadableStreamDefaultControllerConstructor;
interface ReadableStreamDefaultReader<T = unknown> extends NOEStdlib.ReadableStreamDefaultReader<T> {}
declare var ReadableStreamDefaultReader: NOEStdlib.ReadableStreamDefaultReaderConstructor;
interface WritableStream<T = unknown> extends NOEStdlib.WritableStream<T> {}
declare var WritableStream: NOEStdlib.WritableStreamConstructor;
interface WritableStreamDefaultWriter<T = unknown> extends NOEStdlib.WritableStreamDefaultWriter<T> {}
declare var WritableStreamDefaultWriter: NOEStdlib.WritableStreamDefaultWriterConstructor;
interface TransformStream<I = unknown, O = unknown> extends NOEStdlib.TransformStream<I, O> {}
declare var TransformStream: NOEStdlib.TransformStreamConstructor;
interface ByteLengthQueuingStrategy extends NOEStdlib.QueuingStrategyObject<Uint8Array> {}
declare var ByteLengthQueuingStrategy: NOEStdlib.QueuingStrategyConstructor<Uint8Array>;
interface CountQueuingStrategy extends NOEStdlib.QueuingStrategyObject<unknown> {}
declare var CountQueuingStrategy: NOEStdlib.QueuingStrategyConstructor<unknown>;
interface TextEncoderStream extends NOEStdlib.TextEncoderStream {}
declare var TextEncoderStream: NOEStdlib.TextEncoderStreamConstructor;
interface TextDecoderStream extends NOEStdlib.TextDecoderStream {}
declare var TextDecoderStream: NOEStdlib.TextDecoderStreamConstructor;
interface CompressionStream extends NOEStdlib.CompressionStream {}
declare var CompressionStream: NOEStdlib.CompressionStreamConstructor;
interface DecompressionStream extends NOEStdlib.DecompressionStream {}
declare var DecompressionStream: NOEStdlib.DecompressionStreamConstructor;
interface Worker extends NOEStdlib.Worker {}
declare var Worker: NOEStdlib.WorkerConstructor;
interface WebSocket extends NOEStdlib.WebSocket {}
declare var WebSocket: NOEStdlib.WebSocketConstructor;
interface CryptoKey extends NOEStdlib.CryptoKey {}
declare var CryptoKey: {prototype: CryptoKey};
interface Crypto extends NOEStdlib.Crypto {}
interface Performance extends NOEStdlib.Performance {}
declare var crypto: NOEStdlib.Crypto;
declare var performance: NOEStdlib.Performance;
declare var console: NOEStdlib.Console;
declare var atob: (data: string) => string;
declare var btoa: (data: string) => string;
declare var structuredClone: <T>(value: T, options?: {transfer?: Iterable<NOEStdlib.Transferable>}) => T;
declare var fetch: typeof NOEStdlib.fetch;
declare var serve: typeof NOEStdlib.serve;
declare var upgradeWebSocket: typeof NOEStdlib.upgradeWebSocket;
declare var process: NOEStdlib.Process;
declare var Buffer: NOEStdlib.BufferConstructor;
declare function queueMicrotask(callback: () => void): void;
declare const setTimeout: NOEStdlib.TimersAPI["setTimeout"];
declare const clearTimeout: NOEStdlib.TimersAPI["clearTimeout"];
declare const setInterval: NOEStdlib.TimersAPI["setInterval"];
declare const clearInterval: NOEStdlib.TimersAPI["clearInterval"];
