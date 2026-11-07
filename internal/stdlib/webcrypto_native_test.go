package stdlib

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"hash"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestWebCryptoNativeOperations(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	native, err := newWebCryptoNative(rt, nil, bytes.NewReader([]byte{1, 2, 3, 4}))
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("native", native); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`
		globalThis.webCryptoNativeVectors = "pending";
		const hex = (bytes) => Array.from(bytes, b => b.toString(16).padStart(2, "0")).join("");
		(async () => {
		  const empty = new Uint8Array(0);
		  const digest = await native.request("digest", "SHA-256", new Uint8Array([97, 98, 99]), empty, empty, empty, 0, 0);
		  const mac = await native.request("hmac", "SHA-256", new Uint8Array(Array.from("what do ya want for nothing?", c => c.charCodeAt(0))), new Uint8Array([74, 101, 102, 101]), empty, empty, 0, 0);
		  const pbkdf2 = await native.request("pbkdf2", "SHA-256", empty, new Uint8Array(Array.from("password", c => c.charCodeAt(0))), new Uint8Array([115, 97, 108, 116]), empty, 1, 32);
		  const hkdf = await native.request("hkdf", "SHA-256", empty, new Uint8Array(22).fill(0x0b),
		    new Uint8Array([0,1,2,3,4,5,6,7,8,9,10,11,12]),
		    new Uint8Array([0xf0,0xf1,0xf2,0xf3,0xf4,0xf5,0xf6,0xf7,0xf8,0xf9]), 0, 42);
		  const random = native.random(4);
		  webCryptoNativeVectors = [hex(digest), hex(mac), hex(pbkdf2), hex(hkdf), hex(random),
		    native.equal(new Uint8Array([1, 2]), new Uint8Array([1, 2])),
		    native.equal(new Uint8Array([1, 2]), new Uint8Array([1, 3]))].join("|");
		})().catch(error => { webCryptoNativeVectors = "error:" + error.message; });
	`); err != nil {
		t.Fatal(err)
	}
	value, err := rt.Get("webCryptoNativeVectors")
	if err != nil {
		t.Fatal(err)
	}
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" +
		"|5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843" +
		"|120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b" +
		"|3cb25f25faacd57a90434f64d0362f2a2d2d0a90cf1a5a4c5db02d56ecc4c5bf34007208d5b887185865" +
		"|01020304|true|false"
	if got := value.String(); got != want {
		t.Fatalf("native crypto vectors = %q, want %q", got, want)
	}
}

func TestWebCryptoNativeLoopSnapshotsInput(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	scope, err := NewExecutionScope(loop, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	native, err := newWebCryptoNative(rt, loop, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("native", native); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`
		globalThis.webCryptoSnapshot = "pending";
		const source = new Uint8Array([97, 98, 99]);
		native.request("digest", "SHA-256", source, new Uint8Array(0), new Uint8Array(0), new Uint8Array(0), 0, 0)
		  .then(bytes => { webCryptoSnapshot = Array.from(bytes, b => b.toString(16).padStart(2, "0")).join(""); });
		source.fill(0);
	`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	value, err := rt.Get("webCryptoSnapshot")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value.String(), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"; got != want {
		t.Fatalf("digest after input mutation = %q, want original snapshot %q", got, want)
	}
	if loop.Pending() {
		t.Fatal("native request left host or result work pending after settlement")
	}
}

func TestWebCryptoNativePBKDF2ScopeCloseCancelsAndJoins(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	scope, err := NewExecutionScope(loop, parent)
	if err != nil {
		t.Fatal(err)
	}
	native, err := newWebCryptoNative(rt, loop, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("native", native); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`
		globalThis.webCryptoCloseResult = "pending";
		native.request("pbkdf2", "SHA-256", new Uint8Array(0), new Uint8Array([112]), new Uint8Array([115]), new Uint8Array(0), 5000000, 32)
		  .then(() => { webCryptoCloseResult = "settled"; }, () => { webCryptoCloseResult = "rejected"; });
	`); err != nil {
		t.Fatal(err)
	}
	// Give the worker time to enter the deliberately long derivation, then
	// close the execution while its native request is still outstanding.
	time.Sleep(10 * time.Millisecond)
	startedClose := time.Now()
	scope.Close()
	if elapsed := time.Since(startedClose); elapsed > time.Second {
		t.Fatalf("closing the execution took %s while cancelling PBKDF2", elapsed)
	}
	ctx, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if loop.Pending() {
		t.Fatal("closed execution retained native or queued promise work")
	}
	value, err := rt.Get("webCryptoCloseResult")
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != "pending" {
		t.Fatalf("closed execution settled its abandoned promise as %q", got)
	}
}

func TestWebCryptoNativeScopeCloseDropsQueuedPromiseResult(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	scope, err := NewExecutionScope(loop, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	promise := rt.NewPromise()
	if err := rt.Set("pendingNativePromise", promise.Value()); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`
		globalThis.webCryptoQueuedResult = "pending";
		pendingNativePromise.then(() => { webCryptoQueuedResult = "settled"; });
	`); err != nil {
		t.Fatal(err)
	}
	pending := &webCryptoPendingResult{rt: rt, promise: promise}
	remove, err := scope.AddCleanupHandle(pending.discard)
	if err != nil {
		t.Fatal(err)
	}
	pending.setCleanup(remove, nil)
	if !pending.setResult([]byte{1, 2, 3}, nil) {
		t.Fatal("pending result rejected setup")
	}
	loop.postExecution(pending.deliver)
	if !loop.Pending() {
		t.Fatal("owner result was not counted as pending work")
	}

	// The result callback is queued but has not run. Closing the scope clears
	// the promise and digest bytes from its pending-result owner; running the
	// loop drains the already posted callback without settling the promise.
	scope.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if loop.Pending() {
		t.Fatal("scope close retained queued result work")
	}
	pending.mu.Lock()
	retainedPromise := pending.promise != nil || pending.rt != nil || pending.result != nil
	pending.mu.Unlock()
	if retainedPromise {
		t.Fatal("scope cleanup retained the abandoned promise or result bytes")
	}
	value, err := rt.Get("webCryptoQueuedResult")
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != "pending" {
		t.Fatalf("queued result settled after scope close as %q", got)
	}
}

type waitingHash struct {
	hash.Hash
	ctx     context.Context
	started chan struct{}
	once    chan struct{}
}

func (h *waitingHash) Write(p []byte) (int, error) {
	select {
	case <-h.once:
	default:
		close(h.once)
		close(h.started)
	}
	<-h.ctx.Done()
	return h.Hash.Write(p)
}

func TestPBKDF2ContextHashCancellationUsesStandardPrimitive(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scope, err := NewExecutionScope(loop, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()

	started := make(chan struct{})
	joined := make(chan error, 1)
	if err := scope.Go(func(ctx context.Context) {
		_, err := pbkdf2WebCryptoKey(ctx, func() hash.Hash {
			return &waitingHash{Hash: sha256.New(), ctx: ctx, started: started, once: make(chan struct{})}
		}, []byte("password"), []byte("salt"), 5_000_000, 32)
		joined <- err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("PBKDF2 did not reach the supplied Go hash primitive")
	}

	// Scope close cancels the custom primitive's wait. The context-aware hash
	// then interrupts the standard-library PBKDF2 call and its worker is joined.
	scope.Close()
	select {
	case err := <-joined:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("PBKDF2 cancellation error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("execution close returned without joining PBKDF2 work")
	}
}
