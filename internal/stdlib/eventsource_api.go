package stdlib

// eventSourceJS is the JavaScript interface and SSE parser for EventSource.
const eventSourceJS = `(function (host) {
  "use strict";

  const URL_ = URL;
  const TextDecoder_ = TextDecoder;
  const Event_ = Event;
  const MessageEvent_ = MessageEvent;
  const EventTarget_ = EventTarget;
  const TypeError_ = TypeError;
  const DOMException_ = DOMException;
  const String_ = String;
  const Number_ = Number;
  const Promise_ = Promise;
  const WeakMap_ = WeakMap;
  const reflectApply = Reflect.apply;
  const defineProperty = Object.defineProperty;
  const freeze = Object.freeze;
  const charCodeAt = String.prototype.charCodeAt;
  const slice = String.prototype.slice;
  const indexOf = String.prototype.indexOf;
  const includes = String.prototype.includes;
  const trim = String.prototype.trim;
  const toLowerCase = String.prototype.toLowerCase;
  const join = Array.prototype.join;
  const dispatch = EventTarget_.prototype.dispatchEvent;
  const eventHandlers = EventTarget_[Symbol.for("noe.stdlib.eventHandlers")];
  const decode = TextDecoder_.prototype.decode;
  const hrefGetter = Object.getOwnPropertyDescriptor(URL_.prototype, "href").get;
  const hashSetter = Object.getOwnPropertyDescriptor(URL_.prototype, "hash").set;
  const originGetter = Object.getOwnPropertyDescriptor(URL_.prototype, "origin").get;
  const connect = host.connect;
  const nextTask = host.nextTask;
  const maxEventBytes = Number_(host.maxEventBytes);
  const bounded = maxEventBytes > 0;
  const setTimeout_ = typeof setTimeout === "function" ? setTimeout : null;
  const clearTimeout_ = typeof clearTimeout === "function" ? clearTimeout : null;

  const CONNECTING = 0, OPEN = 1, CLOSED = 2;
  const MAX_TIMEOUT = 2147483647;
  const states = new WeakMap_();
  const overflow = freeze({eventSourceFatal: true});

  function apply(fn, receiver, args) {
    return reflectApply(fn, receiver, args);
  }

  function stateOf(source) {
    const state = states.get(source);
    if (state === undefined) throw new TypeError_("Illegal invocation");
    return state;
  }

  function stringAt(text, index) {
    return apply(charCodeAt, text, [index]);
  }

  function stringSlice(text, start, end) {
    return apply(slice, text, end === undefined ? [start] : [start, end]);
  }

  function utf8Length(text) {
    let bytes = 0;
    for (const character of text) {
      if (character.length === 2) {
        bytes += 4;
      } else {
        const code = stringAt(character, 0);
        bytes += code < 0x80 ? 1 : code < 0x800 ? 2 : 3;
      }
    }
    return bytes;
  }

  function checkLimit(parser) {
    if (bounded && parser.partialBytes + parser.lineBytes > maxEventBytes) {
      throw overflow;
    }
  }

  function callNoThrow(fn, receiver) {
    if (typeof fn !== "function") return;
    try { apply(fn, receiver, []); } catch (_) {}
  }

  function cancelResponse(response) {
    if (response === null || response === undefined) return;
    let cancel;
    try { cancel = response.cancel; } catch (_) { return; }
    callNoThrow(cancel, response);
  }

  function cancelCurrent(state) {
    const cancel = state.cancel;
    const response = state.response;
    state.cancel = null;
    state.response = null;
    callNoThrow(cancel, undefined);
    cancelResponse(response);
  }

  function releaseCurrent(state, response) {
    if (state.response === response) state.response = null;
    state.cancel = null;
  }

  function fire(source, event) {
    stateOf(source);
    apply(dispatch, source, [event]);
  }

  function installHandler(name, type) {
    defineProperty(EventSource.prototype, name, {
      enumerable: true,
      configurable: true,
      get() {
        stateOf(this);
        return apply(eventHandlers.get, eventHandlers, [this, type]);
      },
      set(value) {
        stateOf(this);
        apply(eventHandlers.set, eventHandlers, [this, type, value]);
      },
    });
  }

  function contentType(response) {
    const headers = response.headers;
    if (headers === null || headers === undefined) return "";
    for (let i = 0; i < headers.length; i++) {
      const pair = headers[i];
      if (pair === null || pair === undefined) continue;
      const name = String_(pair[0]);
      if (apply(toLowerCase, name, []) === "content-type") return String_(pair[1]);
    }
    return "";
  }

  function isEventStream(response) {
    const header = contentType(response);
    const semicolon = apply(indexOf, header, [";"]);
    const essence = apply(trim, semicolon < 0 ? header : stringSlice(header, 0, semicolon), []);
    return apply(toLowerCase, essence, []) === "text/event-stream";
  }

  function finalOrigin(responseURL, fallback) {
    try {
      const parsed = new URL_(String_(responseURL || fallback));
      return apply(originGetter, parsed, []);
    } catch (_) {
      return fallback;
    }
  }

  function makeParser(lastEventID) {
    return {
      decoder: new TextDecoder_("utf-8", {ignoreBOM: true}),
      firstCharacter: true,
      line: "",
      lineBytes: 0,
      data: [],
      event: "",
      id: lastEventID,
      partialBytes: utf8Length(lastEventID),
      skipLF: false,
    };
  }

  function decodeChunk(parser, bytes) {
    let text = apply(decode, parser.decoder, [bytes, {stream: true}]);
    if (parser.firstCharacter && text.length !== 0) {
      parser.firstCharacter = false;
      if (stringAt(text, 0) === 0xfeff) text = stringSlice(text, 1);
    }
    return text;
  }

  function digitsOnly(text) {
    if (text.length === 0) return false;
    for (let i = 0; i < text.length; i++) {
      const code = stringAt(text, i);
      if (code < 0x30 || code > 0x39) return false;
    }
    return true;
  }

  async function dispatchMessage(source, parser, origin) {
    const state = stateOf(source);
    state.lastEventID = parser.id;
    if (parser.data.length === 0) {
      parser.event = "";
      parser.partialBytes = utf8Length(parser.id);
      return true;
    }

    const data = apply(join, parser.data, ["\n"]);
    const type = parser.event === "" ? "message" : parser.event;
    parser.data = [];
    parser.event = "";
    parser.partialBytes = utf8Length(parser.id);

    await nextTask();
    if (state.closed) return false;
    fire(source, new MessageEvent_(type, {
      data,
      origin,
      lastEventId: state.lastEventID,
    }));
    // Let promise jobs queued by listeners run before another event from this
    // network chunk is dispatched.
    await 0;
    return !state.closed;
  }

  async function processLine(source, parser, origin) {
    const line = parser.line;
    parser.line = "";
    parser.lineBytes = 0;

    if (line === "") return dispatchMessage(source, parser, origin);
    if (stringAt(line, 0) === 0x3a) return true;

    const colon = apply(indexOf, line, [":"]);
    const field = colon < 0 ? line : stringSlice(line, 0, colon);
    let value = colon < 0 ? "" : stringSlice(line, colon + 1);
    if (stringAt(value, 0) === 0x20) value = stringSlice(value, 1);

    if (field === "data") {
      parser.data.push(value);
      parser.partialBytes += utf8Length(value) + 1;
    } else if (field === "event") {
      parser.partialBytes -= utf8Length(parser.event);
      parser.event = value;
      parser.partialBytes += utf8Length(parser.event);
    } else if (field === "id") {
      if (!apply(includes, value, ["\0"])) {
        parser.partialBytes -= utf8Length(parser.id);
        parser.id = value;
        parser.partialBytes += utf8Length(parser.id);
      }
    } else if (field === "retry" && digitsOnly(value)) {
      const parsed = Number_(value);
      stateOf(source).retry = parsed > MAX_TIMEOUT ? MAX_TIMEOUT : parsed;
    }
    if (bounded && parser.partialBytes > maxEventBytes) throw overflow;
    return true;
  }

  async function consumeText(source, parser, origin, text) {
    const state = stateOf(source);
    for (const character of text) {
      if (parser.skipLF) {
        parser.skipLF = false;
        if (character === "\n") continue;
      }
      if (character === "\r") {
        if (!(await processLine(source, parser, origin))) return false;
        parser.skipLF = true;
        if (state.closed) return false;
        continue;
      }
      if (character === "\n") {
        if (!(await processLine(source, parser, origin))) return false;
        if (state.closed) return false;
        continue;
      }
      parser.line += character;
      parser.lineBytes += utf8Length(character);
      checkLimit(parser);
    }
    return !state.closed;
  }

  function registerCancel(state, cancel) {
    if (typeof cancel !== "function") return;
    if (state.closed) {
      callNoThrow(cancel, undefined);
      return;
    }
    state.cancel = cancel;
  }

  function fatalError(error) {
    if (error === overflow) return true;
    try { return error !== null && error !== undefined && error.eventSourceFatal === true; }
    catch (_) { return false; }
  }

  async function reportError(source, terminal) {
    const state = stateOf(source);
    if (terminal) cancelCurrent(state);
    await nextTask();
    if (state.closed) return;
    if (terminal) {
      state.readyState = CLOSED;
      state.closed = true;
    } else {
      state.readyState = CONNECTING;
    }
    fire(source, new Event_("error"));
    await 0;
  }

  function waitToReconnect(state) {
    if (state.closed) return Promise_.resolve();
    return new Promise_(resolve => {
      let settled = false;
      const finish = () => {
        if (settled) return;
        settled = true;
        state.timer = null;
        state.timerResolve = null;
        resolve();
      };
      state.timerResolve = finish;
      if (setTimeout_ === null) {
        // EventSource is installed with a loop and timers in normal use. Keep
        // a task boundary if a host deliberately omits the timer globals.
        nextTask().then(finish, finish);
      } else {
        const delay = state.retry > MAX_TIMEOUT ? MAX_TIMEOUT : state.retry;
        state.timer = setTimeout_(finish, delay);
      }
    });
  }

  async function drive(source) {
    const state = stateOf(source);
    while (!state.closed) {
      let response = null;
      let terminal = false;
      try {
        response = await connect(state.requestURL, state.withCredentials,
          state.lastEventID, cancel => registerCancel(state, cancel));
        if (state.closed) {
          cancelResponse(response);
          return;
        }
        state.response = response;
        if (response.status !== 200 || !isEventStream(response)) {
          terminal = true;
        } else {
          const origin = finalOrigin(response.url, state.origin);
          const parser = makeParser(state.lastEventID);
          await nextTask();
          if (state.closed) return;
          state.readyState = OPEN;
          fire(source, new Event_("open"));
          await 0;
          if (state.closed) return;

          for (;;) {
            const bytes = await response.read();
            if (state.closed) return;
            if (bytes === null || bytes === undefined) {
              // Flush a split or malformed final UTF-8 sequence, then discard
              // the unfinished line and event as the stream algorithm does.
              apply(decode, parser.decoder, [undefined]);
              break;
            }
            const text = decodeChunk(parser, bytes);
            if (!(await consumeText(source, parser, origin, text))) return;
          }
          releaseCurrent(state, response);
        }
      } catch (error) {
        if (state.closed) {
          cancelCurrent(state);
          return;
        }
        terminal = fatalError(error);
        cancelCurrent(state);
      }

      if (state.closed) return;
      if (terminal) {
        await reportError(source, true);
        return;
      }
      await reportError(source, false);
      if (state.closed) return;
      await waitToReconnect(state);
    }
  }

  class EventSource extends EventTarget_ {
    constructor(url, options = {}) {
      if (arguments.length === 0) throw new TypeError_("EventSource requires a URL");
      super();

      if (typeof url === "symbol") throw new TypeError_("EventSource URL cannot be a Symbol");
      if (options != null && typeof options !== "object" && typeof options !== "function") {
        throw new TypeError_("EventSource options must be a dictionary");
      }
      const input = String_(url);
      let parsed;
      try {
        parsed = new URL_(input);
      } catch (_) {
        throw new DOMException_("The URL is invalid", "SyntaxError");
      }
      const serialized = apply(hrefGetter, parsed, []);
      const request = new URL_(serialized);
      apply(hashSetter, request, [""]);
      const requestURL = apply(hrefGetter, request, []);
      const withCredentials = options == null ? false : !!options.withCredentials;
      const origin = apply(originGetter, parsed, []);
      states.set(this, {
        url: serialized,
        requestURL,
        origin,
        withCredentials,
        readyState: CONNECTING,
        lastEventID: "",
        retry: 3000,
        closed: false,
        cancel: null,
        response: null,
        timer: null,
        timerResolve: null,
      });
      drive(this);
    }

    get url() { return stateOf(this).url; }
    get withCredentials() { return stateOf(this).withCredentials; }
    get readyState() { return stateOf(this).readyState; }

    close() {
      const state = stateOf(this);
      if (state.closed || state.readyState === CLOSED) return;
      state.closed = true;
      state.readyState = CLOSED;
      if (state.timer !== null && clearTimeout_ !== null) {
        try { clearTimeout_(state.timer); } catch (_) {}
        state.timer = null;
      }
      const resolve = state.timerResolve;
      state.timerResolve = null;
      if (resolve !== null) resolve();
      cancelCurrent(state);
    }
  }

  installHandler("onopen", "open");
  installHandler("onmessage", "message");
  installHandler("onerror", "error");

  for (const [name, value] of [["CONNECTING", CONNECTING], ["OPEN", OPEN], ["CLOSED", CLOSED]]) {
    defineProperty(EventSource, name, {value, enumerable: true});
    defineProperty(EventSource.prototype, name, {value, enumerable: true});
  }
  defineProperty(EventSource.prototype, Symbol.toStringTag, {value: "EventSource", configurable: true});

  return {EventSource};
})`
