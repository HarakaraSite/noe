package stdlib

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// maxInflated bounds what one decompression may produce, so that a small input
// cannot ask for all the memory there is. A script that means to decompress
// more than this should do it in pieces.
const maxInflated = 1 << 30

// Compression installs the web's CompressionStream and DecompressionStream and
// node's zlib module.
//
// Compressing is arithmetic -- it reads bytes and writes bytes -- so it needs
// no capability, and the formats are the three that HTTP uses: gzip, zlib's
// deflate, and raw deflate.
//
//	const packed = await new Response(
//	    stream.pipeThrough(new CompressionStream("gzip"))).arrayBuffer()
//
//	import {gzipSync, gunzipSync} from "zlib"
func Compression(rt *quickjs.Runtime) error {
	return compression(rt, nil)
}

func compression(rt *quickjs.Runtime, loop *Loop) error {
	c := &compressHost{rt: rt, loop: loop}
	host := rt.NewObject()
	if err := setAll(host, map[string]any{
		"deflate":  c.deflate,
		"inflate":  c.inflate,
		"packer":   c.packer,
		"unpacker": c.unpacker,
	}); err != nil {
		return err
	}

	api, err := evalWithHost(rt, "<compress>", compressJS, host)
	if err != nil {
		return err
	}
	for _, name := range []string{"CompressionStream", "DecompressionStream"} {
		v, err := api.Get(name)
		if err != nil {
			return err
		}
		if err := rt.Set(name, v); err != nil {
			return err
		}
	}
	zlibAPI, err := api.Get("zlib")
	if err != nil {
		return err
	}
	exports, err := moduleExports(zlibAPI)
	if err != nil {
		return err
	}
	if err := rt.SetModuleValues("zlib", exports); err != nil {
		return err
	}
	return rt.SetModuleValues("node:zlib", exports)
}

type compressHost struct {
	rt   *quickjs.Runtime
	loop *Loop
}

// writerFor is the one place a format name becomes a compressor. The names are
// the web's, which are also the three framings HTTP uses.
func writerFor(format string, out io.Writer) (io.WriteCloser, error) {
	switch format {
	case "gzip":
		return gzip.NewWriter(out), nil
	case "deflate":
		return zlib.NewWriter(out), nil
	case "deflate-raw":
		return flate.NewWriter(out, flate.DefaultCompression)
	}
	return nil, errors.New("unsupported compression format: " + format)
}

func readerFor(format string, in io.Reader) (io.ReadCloser, error) {
	switch format {
	case "gzip":
		return gzip.NewReader(in)
	case "deflate":
		return zlib.NewReader(in)
	case "deflate-raw":
		return io.NopCloser(flate.NewReader(in)), nil
	}
	return nil, errors.New("unsupported compression format: " + format)
}

// compressionByteReader bounds input read-ahead and keeps prefetched bytes
// visible for the Web streams trailing-data check. Header validation and full
// buffer consumption are the only points that acknowledge a write's bytes.
type compressionByteReader struct {
	reader     io.Reader
	buffer     []byte
	err        error
	readBytes  int64
	ackedBytes int64
	onProgress func(int64)
}

func (r *compressionByteReader) fill() error {
	if len(r.buffer) == 0 {
		r.ackProgress(r.readBytes)
	}
	_, err := r.peek(1)
	return err
}

func (r *compressionByteReader) ackProgress(bytes int64) {
	if bytes > r.ackedBytes {
		r.ackedBytes = bytes
		if r.onProgress != nil {
			r.onProgress(bytes)
		}
	}
}

func (r *compressionByteReader) peek(size int) ([]byte, error) {
	for len(r.buffer) < size {
		if r.err != nil {
			if len(r.buffer) != 0 {
				return r.buffer, io.ErrUnexpectedEOF
			}
			return nil, r.err
		}
		buffer := make([]byte, 4<<10)
		n, err := r.reader.Read(buffer)
		if n > 0 {
			r.buffer = append(r.buffer, buffer[:n]...)
			r.readBytes += int64(n)
		}
		if err != nil {
			r.err = err
		}
	}
	return r.buffer[:size], nil
}

func (r *compressionByteReader) peekByteAt(index int) (byte, error) {
	buffer, err := r.peek(index + 1)
	if err != nil {
		return 0, err
	}
	return buffer[index], nil
}

func (r *compressionByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := r.fill(); err != nil {
		return 0, err
	}
	n := copy(p, r.buffer)
	r.buffer = r.buffer[n:]
	return n, nil
}

func (r *compressionByteReader) ReadByte() (byte, error) {
	if err := r.fill(); err != nil {
		return 0, err
	}
	b := r.buffer[0]
	r.buffer = r.buffer[1:]
	return b, nil
}

func streamReaderFor(format string, in io.Reader, onProgress func(int64)) (io.ReadCloser, *compressionByteReader, error) {
	source := &compressionByteReader{reader: in, onProgress: onProgress}
	switch format {
	case "gzip":
		// Validate each fixed-header byte before acknowledging it. That keeps
		// byte-at-a-time writers moving while ensuring a reserved FLG bit fails
		// the write that supplied it.
		for i := 0; i < 10; i++ {
			b, err := source.peekByteAt(i)
			if err != nil {
				return nil, source, err
			}
			switch i {
			case 0:
				if b != 0x1f {
					return nil, source, errors.New("invalid gzip header")
				}
			case 1:
				if b != 0x8b {
					return nil, source, errors.New("invalid gzip header")
				}
			case 2:
				if b != 8 {
					return nil, source, errors.New("unsupported gzip compression method")
				}
			case 3:
				if b&0xe0 != 0 {
					return nil, source, errors.New("gzip header has non-zero reserved flags")
				}
			}
			source.ackProgress(int64(i + 1))
		}
		r, err := gzip.NewReader(source)
		if err != nil {
			return nil, source, err
		}
		r.Multistream(false)
		return r, source, nil
	case "deflate":
		// FDICT is unsupported even when Go's zlib reader could match the
		// dictionary checksum against its empty default dictionary.
		cmf, err := source.peekByteAt(0)
		if err != nil {
			return nil, source, err
		}
		if cmf&0x0f != 8 || cmf>>4 > 7 {
			return nil, source, errors.New("invalid zlib header")
		}
		source.ackProgress(1)
		flg, err := source.peekByteAt(1)
		if err != nil {
			return nil, source, err
		}
		if flg&0x20 != 0 {
			return nil, source, errors.New("zlib preset dictionaries are not supported")
		}
		if ((uint16(cmf)<<8)|uint16(flg))%31 != 0 {
			return nil, source, errors.New("invalid zlib header check bits")
		}
		source.ackProgress(2)
		r, err := zlib.NewReader(source)
		return r, source, err
	case "deflate-raw":
		return io.NopCloser(flate.NewReader(source)), source, nil
	default:
		return nil, source, errors.New("unsupported compression format: " + format)
	}
}

func (c *compressHost) deflate(format string, data quickjs.Value) (quickjs.Value, error) {
	b, ok := data.Bytes()
	if !ok {
		return quickjs.Value{}, c.rt.Throw(c.rt.NewError("TypeError",
			"the data must be a typed array or an ArrayBuffer"))
	}
	var out bytes.Buffer
	w, err := writerFor(format, &out)
	if err != nil {
		return quickjs.Value{}, c.rt.Throw(c.rt.NewError("TypeError", err.Error()))
	}
	if _, err := w.Write(b); err != nil {
		return quickjs.Value{}, err
	}
	if err := w.Close(); err != nil {
		return quickjs.Value{}, err
	}
	return c.rt.NewBytes(out.Bytes()), nil
}

func (c *compressHost) inflate(format string, data quickjs.Value) (quickjs.Value, error) {
	b, ok := data.Bytes()
	if !ok {
		return quickjs.Value{}, c.rt.Throw(c.rt.NewError("TypeError",
			"the data must be a typed array or an ArrayBuffer"))
	}
	r, err := readerFor(format, bytes.NewReader(b))
	if err != nil {
		return quickjs.Value{}, c.rt.Throw(c.rt.NewError("Error", err.Error()))
	}
	defer r.Close()
	// One more byte than the limit, so that reaching it is told apart from
	// happening to end there.
	out, err := io.ReadAll(io.LimitReader(r, maxInflated+1))
	if err != nil {
		return quickjs.Value{}, c.rt.Throw(c.rt.NewError("Error",
			"the data could not be decompressed: "+err.Error()))
	}
	if len(out) > maxInflated {
		return quickjs.Value{}, c.rt.Throw(c.rt.NewError("RangeError",
			"the decompressed data is larger than this runtime will produce at once"))
	}
	return c.rt.NewBytes(out), nil
}

// packer hands back a compressor that keeps its state between calls, which is
// what lets a stream be compressed as it goes rather than gathered first.
//
// The asynchronous counterpart for decompression is in compress_stream.go.
func (c *compressHost) packer(format string) (quickjs.Value, error) {
	var out bytes.Buffer
	w, err := writerFor(format, &out)
	if err != nil {
		return quickjs.Value{}, c.rt.Throw(c.rt.NewError("TypeError", err.Error()))
	}
	closed := false
	take := func() quickjs.Value {
		// NewBytes copies into the runtime before the buffer is reused.
		b := c.rt.NewBytes(out.Bytes())
		out.Reset()
		return b
	}
	o := c.rt.NewObject()
	err = setAll(o, map[string]any{
		"write": func(data quickjs.Value) (quickjs.Value, error) {
			if closed {
				return quickjs.Value{}, c.rt.Throw(c.rt.NewError("TypeError",
					"this compressor has already finished"))
			}
			b, ok := data.Bytes()
			if !ok {
				return quickjs.Value{}, c.rt.Throw(c.rt.NewError("TypeError",
					"a compression stream takes bytes"))
			}
			if _, err := w.Write(b); err != nil {
				return quickjs.Value{}, err
			}
			return take(), nil
		},
		"close": func() (quickjs.Value, error) {
			if closed {
				return c.rt.NewBytes(nil), nil
			}
			closed = true
			if err := w.Close(); err != nil {
				return quickjs.Value{}, err
			}
			return take(), nil
		},
	})
	if err != nil {
		return quickjs.Value{}, err
	}
	return o, nil
}

// compressJS is the two stream classes and the zlib module.
const compressJS = `(function (host) {
  "use strict";

  const apply = Reflect.apply;
  const String_ = String;
  const TypeError_ = TypeError;
  const defineProperty = Object.defineProperty;
  const ownDescriptor = Object.getOwnPropertyDescriptor;
  const weakMapHas = WeakMap.prototype.has;
  const weakMapGet = WeakMap.prototype.get;
  const weakMapSet = WeakMap.prototype.set;
  const uint8Array = Uint8Array;
  const isView = ArrayBuffer.isView;
  const arrayBufferByteLength = ownDescriptor(ArrayBuffer.prototype, "byteLength").get;
  const arrayBufferResizable = ownDescriptor(ArrayBuffer.prototype, "resizable")?.get;
  const arrayBufferDetached = ownDescriptor(ArrayBuffer.prototype, "detached")?.get;
  const typedArrayPrototype = Object.getPrototypeOf(Uint8Array.prototype);
  const typedArrayBuffer = ownDescriptor(typedArrayPrototype, "buffer").get;
  const typedArrayByteOffset = ownDescriptor(typedArrayPrototype, "byteOffset").get;
  const typedArrayByteLength = ownDescriptor(typedArrayPrototype, "byteLength").get;
  const dataViewPrototype = DataView.prototype;
  const dataViewBuffer = ownDescriptor(dataViewPrototype, "buffer").get;
  const dataViewByteOffset = ownDescriptor(dataViewPrototype, "byteOffset").get;
  const dataViewByteLength = ownDescriptor(dataViewPrototype, "byteLength").get;
  const sharedByteLength = typeof SharedArrayBuffer === "undefined" ? null
    : ownDescriptor(SharedArrayBuffer.prototype, "byteLength").get;
  const sharedGrowable = typeof SharedArrayBuffer === "undefined" ? null
    : ownDescriptor(SharedArrayBuffer.prototype, "growable")?.get;
  const compressionSlots = new WeakMap();
  const decompressionSlots = new WeakMap();

  function call(fn, receiver, args) {
    return apply(fn, receiver, args);
  }

  function domString(value) {
    if (typeof value === "symbol") {
      throw new TypeError_("A Symbol cannot be converted to DOMString");
    }
    return String_(value);
  }

  function formatArgument(args, name) {
    if (args.length === 0) {
      throw new TypeError_(name + " requires a format argument");
    }
    const format = domString(args[0]);
    if (format !== "gzip" && format !== "deflate" && format !== "deflate-raw") {
      throw new TypeError_("Unsupported compression format: " + format);
    }
    return format;
  }

  function streamSlot(slots, receiver, name) {
    if (!call(weakMapHas, slots, [receiver])) {
      throw new TypeError_("The receiver is not a " + name);
    }
    return call(weakMapGet, slots, [receiver]);
  }

  function bufferInfo(buffer) {
    let shared = false;
    let byteLength;
    if (sharedByteLength !== null) {
      try {
        byteLength = call(sharedByteLength, buffer, []);
        shared = true;
      } catch (_) {}
    }
    if (!shared) {
      try {
        byteLength = call(arrayBufferByteLength, buffer, []);
      } catch (_) {
        throw new TypeError_("The chunk is not a valid BufferSource");
      }
    }
    let variableLength = false;
    try {
      const getter = shared ? sharedGrowable : arrayBufferResizable;
      if (getter !== null && getter !== undefined) {
        variableLength = call(getter, buffer, []);
      }
    } catch (_) {
      throw new TypeError_("The chunk buffer is detached or invalid");
    }
    if (variableLength) {
      throw new TypeError_("Resizable and growable buffers are not supported");
    }
    return {byteLength, shared};
  }

  function isDetached(buffer, shared) {
    if (shared) return false;
    if (arrayBufferDetached !== null && arrayBufferDetached !== undefined) {
      try { return call(arrayBufferDetached, buffer, []); }
      catch (_) { return true; }
    }
    try {
      new uint8Array(buffer, 0, 0);
      return false;
    } catch (_) {
      return true;
    }
  }

  function sourceParts(value) {
    if (call(isView, ArrayBuffer, [value])) {
      let buffer;
      try {
        buffer = call(dataViewBuffer, value, []);
      } catch (_) {
        try { buffer = call(typedArrayBuffer, value, []); }
        catch (_) { throw new TypeError_("The chunk is not a valid BufferSource"); }
      }
      const info = bufferInfo(buffer);
      if (isDetached(buffer, info.shared)) {
        return {buffer, offset: 0, byteLength: 0, shared: false, detached: true};
      }
      let offset, byteLength;
      try {
        offset = call(dataViewByteOffset, value, []);
        byteLength = call(dataViewByteLength, value, []);
      } catch (_) {
        try {
          offset = call(typedArrayByteOffset, value, []);
          byteLength = call(typedArrayByteLength, value, []);
        } catch (_) {
          throw new TypeError_("The chunk is not a valid BufferSource");
        }
      }
      return {buffer, offset, byteLength, shared: info.shared};
    }
    const info = bufferInfo(value);
    return {buffer: value, offset: 0, byteLength: info.byteLength, shared: info.shared};
  }

  function asBufferSource(value) {
    const parts = sourceParts(value);
    if (parts.detached || isDetached(parts.buffer, parts.shared)) return new uint8Array(0);
    if (parts.shared) {
      throw new TypeError_("SharedArrayBuffer-backed BufferSource values are not supported");
    }
    let source;
    try {
      source = new uint8Array(parts.buffer, parts.offset, parts.byteLength);
    } catch (_) {
      if (isDetached(parts.buffer, parts.shared)) return new uint8Array(0);
      throw new TypeError_("The chunk buffer is invalid");
    }
    const copy = new uint8Array(parts.byteLength);
    for (let i = 0; i < parts.byteLength; i++) copy[i] = source[i];
    return copy;
  }

  const asBytes = (chunk) => {
    if (chunk instanceof Uint8Array) return chunk;
    if (ArrayBuffer.isView(chunk)) {
      return new Uint8Array(chunk.buffer, chunk.byteOffset, chunk.byteLength);
    }
    if (chunk instanceof ArrayBuffer) return new Uint8Array(chunk);
    if (typeof chunk === "string") return new TextEncoder().encode(chunk);
    throw new TypeError("a compression stream takes bytes");
  };

  // A compressor is fed as the data arrives, so a stream through it never
  // holds more than what has not been written out yet.
  class CompressionStream {
    constructor(format) {
      format = formatArgument(arguments, "CompressionStream");
      host.cloneAs(this, "unsupported");
      const packer = host.packer(format);
      const inner = new TransformStream({
        transform(chunk, controller) {
          const out = packer.write(asBufferSource(chunk));
          if (out.length > 0) controller.enqueue(out);
        },
        flush(controller) {
          const out = packer.close();
          if (out.length > 0) controller.enqueue(out);
        },
      });
      call(weakMapSet, compressionSlots, [this, {
        readable: inner.readable, writable: inner.writable,
      }]);
    }
    get readable() { return streamSlot(compressionSlots, this, "CompressionStream").readable; }
    get writable() { return streamSlot(compressionSlots, this, "CompressionStream").writable; }
  }

  // Go owns the decoder and its pipes; only byte snapshots cross goroutines.
  // Reading drives output, so a slow consumer holds input writes back too.
  class DecompressionStream {
    constructor(format) {
      format = formatArgument(arguments, "DecompressionStream");
      host.cloneAs(this, "unsupported");
      const unpacker = host.unpacker(format);
      let readableController, writableController, failed = false, failure;
      const remember = reason => {
        if (!failed) { failed = true; failure = reason; }
      };
      const streamFailure = error => {
        if (error && error.name === "RangeError") return error;
        const message = error && typeof error.message === "string"
          ? error.message : "The compressed data is invalid";
        return new TypeError_(message);
      };
      const fail = reason => {
        if (failed) return;
        remember(streamFailure(reason));
        readableController.error(failure);
        writableController.error(failure);
        unpacker.abort();
      };
      const readable = new ReadableStream({
        start(controller) { readableController = controller; },
        async pull(controller) {
          try {
            const chunk = await unpacker.read();
            if (failed) return;
            if (chunk === null) controller.close();
            else controller.enqueue(chunk);
          } catch (error) { fail(error); }
        },
        cancel(reason) {
          remember(reason);
          writableController.error(reason);
          unpacker.abort();
        },
      }, {highWaterMark: 0});
      const writable = new WritableStream({
        start(controller) { writableController = controller; },
        async write(chunk) {
          try { await unpacker.write(asBufferSource(chunk)); }
          catch (error) { fail(error); throw failure; }
        },
        async close() {
          try { await unpacker.close(); }
          catch (error) { fail(error); throw failure; }
        },
        abort(reason) {
          remember(reason);
          readableController.error(reason);
          unpacker.abort();
        },
      });
      unpacker.finished.catch(fail);
      call(weakMapSet, decompressionSlots, [this, {readable, writable}]);
    }
    get readable() { return streamSlot(decompressionSlots, this, "DecompressionStream").readable; }
    get writable() { return streamSlot(decompressionSlots, this, "DecompressionStream").writable; }
  }

  for (const [prototype, name] of [
    [CompressionStream.prototype, "CompressionStream"],
    [DecompressionStream.prototype, "DecompressionStream"],
  ]) {
    for (const attribute of ["readable", "writable"]) {
      const descriptor = ownDescriptor(prototype, attribute);
      descriptor.enumerable = true;
      defineProperty(prototype, attribute, descriptor);
    }
    defineProperty(prototype, Symbol.toStringTag, {value: name, configurable: true});
  }

  // --- zlib -----------------------------------------------------------------

  const wrap = (bytes) => typeof Buffer !== "undefined" ? Buffer.from(bytes) : bytes;
  const input = (data) => typeof data === "string"
    ? new TextEncoder().encode(data) : asBytes(data);

  // node's functions take a callback; the work is not slow enough to need one,
  // but code written for node passes one, so both forms are here.
  function pair(name, fn) {
    const sync = (data, options) => wrap(fn(input(data)));
    const async = (data, options, callback) => {
      if (typeof options === "function") { callback = options; options = undefined; }
      queueMicrotask(() => {
        try { callback(null, sync(data, options)); }
        catch (e) { callback(e); }
      });
    };
    return {[name + "Sync"]: sync, [name]: async};
  }

  const zlib = Object.assign(
    {},
    pair("gzip", (b) => host.deflate("gzip", b)),
    pair("gunzip", (b) => host.inflate("gzip", b)),
    pair("deflate", (b) => host.deflate("deflate", b)),
    pair("inflate", (b) => host.inflate("deflate", b)),
    pair("deflateRaw", (b) => host.deflate("deflate-raw", b)),
    pair("inflateRaw", (b) => host.inflate("deflate-raw", b)),
    {
      // unzip accepts either of the two framings, which is what makes it
      // useful for something that arrived over HTTP.
      unzipSync(data) {
        const bytes = input(data);
        try { return wrap(host.inflate("gzip", bytes)); }
        catch (e) { return wrap(host.inflate("deflate", bytes)); }
      },
      constants: {
        Z_NO_COMPRESSION: 0, Z_BEST_SPEED: 1, Z_BEST_COMPRESSION: 9,
        Z_DEFAULT_COMPRESSION: -1,
      },
      createGzip: () => new CompressionStream("gzip"),
      createGunzip: () => new DecompressionStream("gzip"),
      createDeflate: () => new CompressionStream("deflate"),
      createInflate: () => new DecompressionStream("deflate"),
      createDeflateRaw: () => new CompressionStream("deflate-raw"),
      createInflateRaw: () => new DecompressionStream("deflate-raw"),
    });
  zlib.unzip = (data, options, callback) => {
    if (typeof options === "function") { callback = options; options = undefined; }
    queueMicrotask(() => {
      try { callback(null, zlib.unzipSync(data)); } catch (e) { callback(e); }
    });
  };
  zlib.default = zlib;

  return {CompressionStream, DecompressionStream, zlib};
})`
