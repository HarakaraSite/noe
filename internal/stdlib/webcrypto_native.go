package stdlib

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"hash"
	"io"
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

var errWebCryptoOperation = errors.New("OperationError: the cryptographic operation failed")

// newWebCryptoNative creates the private Go boundary used by the Web Crypto
// facade. It deliberately accepts prepared byte arrays: Web IDL conversion,
// key policy, and dictionary validation belong to the facade.
func newWebCryptoNative(rt *quickjs.Runtime, loop *Loop, random io.Reader) (quickjs.Value, error) {
	if rt == nil {
		return quickjs.Value{}, errors.New("stdlib: Web Crypto needs a runtime")
	}
	if loop != nil && !loop.Owns(rt) {
		return quickjs.Value{}, errors.New("stdlib: Web Crypto loop belongs to another runtime")
	}
	if random == nil {
		random = rand.Reader
	}

	host := rt.NewObject()
	if err := setAll(host, map[string]any{
		"request": func(operation, name string, data, key, salt, info quickjs.Value, iterations, length uint32) quickjs.Value {
			return requestWebCrypto(rt, loop, operation, name, data, key, salt, info, iterations, length)
		},
		"random": func(size uint32) (quickjs.Value, error) {
			// Randomness is synchronous by contract, and this function is called
			// on the runtime owner. Keep the reader borrowed and never move it to
			// a worker goroutine.
			if size > 65536 {
				return quickjs.Value{}, rt.Throw(rt.NewError("QuotaExceededError",
					"getRandomValues may not be asked for more than 65536 bytes"))
			}
			out := make([]byte, int(size))
			if _, err := io.ReadFull(random, out); err != nil {
				return quickjs.Value{}, err
			}
			return rt.NewBytes(out), nil
		},
		"equal": func(a, b quickjs.Value) bool {
			ab, aok := a.Bytes()
			bb, bok := b.Bytes()
			return aok && bok && subtle.ConstantTimeCompare(ab, bb) == 1
		},
	}); err != nil {
		return quickjs.Value{}, err
	}
	return host, nil
}

type webCryptoNativeRequest struct {
	operation  string
	name       string
	data       []byte
	key        []byte
	salt       []byte
	info       []byte
	iterations uint32
	length     uint32
}

// requestWebCrypto snapshots every BufferSource while QuickJS is on its owner,
// then lets only those Go byte slices cross to computation work.
func requestWebCrypto(rt *quickjs.Runtime, loop *Loop, operation, name string, data, key, salt, info quickjs.Value, iterations, length uint32) quickjs.Value {
	p := rt.NewPromise()
	request, ok := snapshotWebCryptoRequest(operation, name, data, key, salt, info, iterations, length)
	if !ok {
		p.RejectError(errWebCryptoOperation)
		return p.Value()
	}
	if loop == nil {
		result, err := runWebCryptoRequest(context.Background(), request)
		settleWebCryptoPromise(rt, p, result, err)
		return p.Value()
	}

	pending := &webCryptoPendingResult{rt: rt, promise: p}
	if scope := loop.executionScope(); scope != nil {
		remove, err := scope.AddCleanupHandle(pending.discard)
		if err != nil {
			pending.discard()
			p.RejectError(errWebCryptoOperation)
			return p.Value()
		}
		pending.setCleanup(remove, nil)
	} else {
		pending.setCleanup(nil, context.AfterFunc(loop.Context(), pending.discard))
	}

	loop.Begin()
	if err := loop.Go(func(ctx context.Context) {
		defer loop.Done()
		result, err := runWebCryptoRequest(ctx, request)
		if ctx.Err() != nil {
			pending.discard()
			return
		}
		if !pending.setResult(result, err) {
			return
		}
		// Only the Go-owned pending state crosses to the owner queue. Its
		// cleanup hook clears promise/result references if the execution closes
		// before the queued result reaches the runtime.
		loop.postExecution(pending.deliver)
	}); err != nil {
		loop.Done()
		pending.discard()
		p.RejectError(errWebCryptoOperation)
	}
	return p.Value()
}

// webCryptoPendingResult owns the promise and its eventual byte result between
// the worker and the Loop owner. Cleanup can drop both without touching
// QuickJS, including when an owner callback was already queued at close.
type webCryptoPendingResult struct {
	mu      sync.Mutex
	rt      *quickjs.Runtime
	promise *quickjs.Promise
	result  []byte
	err     error
	remove  func()
	stop    func() bool
}

func (p *webCryptoPendingResult) setCleanup(remove func(), stop func() bool) {
	p.mu.Lock()
	if p.promise != nil {
		p.remove = remove
		p.stop = stop
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	if remove != nil {
		remove()
	}
	if stop != nil {
		stop()
	}
}

func (p *webCryptoPendingResult) setResult(result []byte, err error) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.promise == nil {
		return false
	}
	p.result = result
	p.err = err
	return true
}

func (p *webCryptoPendingResult) discard() {
	p.mu.Lock()
	remove, stop := p.remove, p.stop
	p.rt = nil
	p.promise = nil
	p.result = nil
	p.err = nil
	p.remove = nil
	p.stop = nil
	p.mu.Unlock()
	if remove != nil {
		remove()
	}
	if stop != nil {
		stop()
	}
}

func (p *webCryptoPendingResult) deliver() {
	p.mu.Lock()
	rt, promise := p.rt, p.promise
	result, err := p.result, p.err
	remove, stop := p.remove, p.stop
	p.rt = nil
	p.promise = nil
	p.result = nil
	p.err = nil
	p.remove = nil
	p.stop = nil
	p.mu.Unlock()
	if remove != nil {
		remove()
	}
	if stop != nil {
		stop()
	}
	if rt != nil && promise != nil {
		settleWebCryptoPromise(rt, promise, result, err)
	}
}

func snapshotWebCryptoRequest(operation, name string, data, key, salt, info quickjs.Value, iterations, length uint32) (webCryptoNativeRequest, bool) {
	copyBytes := func(v quickjs.Value) ([]byte, bool) {
		b, ok := v.Bytes()
		if !ok {
			return nil, false
		}
		return append([]byte(nil), b...), true
	}
	dataBytes, ok := copyBytes(data)
	if !ok {
		return webCryptoNativeRequest{}, false
	}
	keyBytes, ok := copyBytes(key)
	if !ok {
		return webCryptoNativeRequest{}, false
	}
	saltBytes, ok := copyBytes(salt)
	if !ok {
		return webCryptoNativeRequest{}, false
	}
	infoBytes, ok := copyBytes(info)
	if !ok {
		return webCryptoNativeRequest{}, false
	}
	return webCryptoNativeRequest{
		operation:  operation,
		name:       name,
		data:       dataBytes,
		key:        keyBytes,
		salt:       saltBytes,
		info:       infoBytes,
		iterations: iterations,
		length:     length,
	}, true
}

func runWebCryptoRequest(ctx context.Context, request webCryptoNativeRequest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch request.operation {
	case "digest":
		makeHash, err := hashFactory(request.name)
		if err != nil {
			return nil, errWebCryptoOperation
		}
		h := &webCryptoContextHash{Hash: makeHash(), ctx: ctx}
		if _, err := h.Write(request.data); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return h.Sum(nil), nil
	case "hmac":
		makeHash, err := hashFactory(request.name)
		if err != nil {
			return nil, errWebCryptoOperation
		}
		contextHash := func() hash.Hash {
			return &webCryptoContextHash{Hash: makeHash(), ctx: ctx}
		}
		h := hmac.New(contextHash, request.key)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := h.Write(request.data); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return h.Sum(nil), nil
	case "pbkdf2":
		if request.iterations == 0 {
			return nil, errWebCryptoOperation
		}
		if request.length == 0 {
			return []byte{}, nil
		}
		iterations, ok := webCryptoNativeInt(request.iterations)
		if !ok {
			return nil, errWebCryptoOperation
		}
		length, ok := webCryptoNativeInt(request.length)
		if !ok {
			return nil, errWebCryptoOperation
		}
		makeHash, err := hashFactory(request.name)
		if err != nil {
			return nil, errWebCryptoOperation
		}
		return pbkdf2WebCryptoKey(ctx, makeHash, request.key, request.salt, iterations, length)
	case "hkdf":
		length, ok := webCryptoNativeInt(request.length)
		if !ok {
			return nil, errWebCryptoOperation
		}
		makeHash, err := hashFactory(request.name)
		if err != nil {
			return nil, errWebCryptoOperation
		}
		contextHash := func() hash.Hash {
			return &webCryptoContextHash{Hash: makeHash(), ctx: ctx}
		}
		result, err := hkdf.Key(contextHash, request.key, request.salt, string(request.info), length)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err != nil {
			return nil, errWebCryptoOperation
		}
		return result, nil
	default:
		return nil, errWebCryptoOperation
	}
}

// webCryptoNativeInt rejects only values the host architecture cannot express
// as int for Go's crypto APIs; it does not apply an output quota.
func webCryptoNativeInt(value uint32) (int, bool) {
	maxInt := uint64(^uint(0) >> 1)
	if uint64(value) > maxInt {
		return 0, false
	}
	return int(value), true
}

func settleWebCryptoPromise(rt *quickjs.Runtime, promise *quickjs.Promise, result []byte, err error) {
	if err != nil {
		promise.RejectError(errWebCryptoOperation)
		return
	}
	if err := promise.Resolve(rt.NewBytes(result)); err != nil {
		promise.RejectError(errWebCryptoOperation)
	}
}

// webCryptoContextHash checks between bounded writes while keeping the hash
// implementation itself in Go's standard library. Its Write errors are used
// by the one-shot hash/HMAC helpers when a close interrupts large inputs.
type webCryptoContextHash struct {
	hash.Hash
	ctx context.Context
}

func (h *webCryptoContextHash) Write(p []byte) (int, error) {
	const chunkSize = 32 * 1024
	written := 0
	for len(p) > 0 {
		if err := h.ctx.Err(); err != nil {
			return written, err
		}
		n := len(p)
		if n > chunkSize {
			n = chunkSize
		}
		count, err := h.Hash.Write(p[:n])
		written += count
		if err != nil {
			return written, err
		}
		if count != n {
			return written, io.ErrShortWrite
		}
		p = p[n:]
	}
	if err := h.ctx.Err(); err != nil {
		return written, err
	}
	return written, nil
}

// pbkdf2Cancellation is a private panic token. crypto/pbkdf2 deliberately has
// no context API, but it calls Reset/Write/Sum on the supplied hash once per
// iteration. The wrapper below checks between those primitive calls, and the
// token is recovered only around the standard-library PBKDF2 operation.
type pbkdf2Cancellation struct{}

type pbkdf2ContextHash struct {
	hash.Hash
	ctx  context.Context
	stop *pbkdf2Cancellation
}

func (h *pbkdf2ContextHash) checkContext() {
	if h.ctx.Err() != nil {
		panic(h.stop)
	}
}

func (h *pbkdf2ContextHash) Reset() {
	h.checkContext()
	h.Hash.Reset()
}

func (h *pbkdf2ContextHash) Write(p []byte) (int, error) {
	const chunkSize = 32 * 1024
	written := 0
	for len(p) > 0 {
		h.checkContext()
		n := len(p)
		if n > chunkSize {
			n = chunkSize
		}
		count, err := h.Hash.Write(p[:n])
		written += count
		if err != nil {
			return written, err
		}
		if count != n {
			return written, io.ErrShortWrite
		}
		p = p[n:]
	}
	h.checkContext()
	return written, nil
}

func (h *pbkdf2ContextHash) Sum(b []byte) []byte {
	h.checkContext()
	return h.Hash.Sum(b)
}

func pbkdf2WebCryptoKey(ctx context.Context, makeHash func() hash.Hash, password, salt []byte, iterations, length int) (result []byte, err error) {
	stop := &pbkdf2Cancellation{}
	checkedHash := func() hash.Hash {
		return &pbkdf2ContextHash{Hash: makeHash(), ctx: ctx, stop: stop}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered == stop {
				result = nil
				err = ctx.Err()
				return
			}
			panic(recovered)
		}
	}()
	return pbkdf2.Key(checkedHash, string(password), salt, iterations, length)
}
