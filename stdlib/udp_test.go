package stdlib_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func executeUDPTest(t *testing.T, code string, profile stdlib.Profile, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[UDP sockets]", Virtual: true, Module: true, Code: code}, profile)
	if err != nil || result.Code != 0 {
		t.Fatalf("UDP result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}

func TestUDPDatagramsPreserveBytesAndRemoteEndpoints(t *testing.T) {
	code := `
import {bind} from "noe:sockets";

function check(condition, message) {
  if (!condition) throw Error(message);
}
function checkRemote(actual, expected, label) {
  check(actual.network === "udp", label + " network=" + actual.network);
  check(actual.host === "127.0.0.1", label + " host=" + actual.host);
  check(actual.port === expected.address.port, label + " port=" + actual.port);
}

const receiver = await bind({network:"udp", host:"127.0.0.1", port:0});
const firstPeer = await bind({network:"udp", host:"127.0.0.1", port:0});
const secondPeer = await bind({network:"udp", host:"127.0.0.1", port:0});
check(receiver.address.network === "udp" && receiver.address.port > 0, "ephemeral bind address");
check(firstPeer.address.port > 0 && secondPeer.address.port > 0, "ephemeral peer addresses");
check(firstPeer.address.port !== secondPeer.address.port, "peers did not receive distinct source ports");

const original = new Uint8Array([0, 255, 1, 128, 0, 42]);
const binaryReceive = receiver.receive();
const binarySend = firstPeer.send(original, receiver.address);
original.fill(9);
if (await binarySend !== 6) throw Error("binary send byte count");
const binary = await binaryReceive;
check(binary.data instanceof Uint8Array, "binary receive type");
check(Array.from(binary.data).join(",") === "0,255,1,128,0,42", "send did not snapshot bytes");
checkRemote(binary.remoteAddress, firstPeer, "first peer endpoint");

const textReceive = receiver.receive();
const textBytes = new TextEncoder().encode("UDP text 日本語");
if (await secondPeer.send(textBytes, receiver.address) !== textBytes.byteLength) throw Error("text send byte count");
const text = await textReceive;
if (new TextDecoder().decode(text.data) !== "UDP text 日本語") throw Error("text datagram changed");
checkRemote(text.remoteAddress, secondPeer, "second peer endpoint");

const emptyReceive = receiver.receive();
if (await firstPeer.send(new Uint8Array(0), receiver.address) !== 0) throw Error("empty datagram send count");
const empty = await emptyReceive;
check(empty.data instanceof Uint8Array && empty.data.byteLength === 0, "empty datagram was not preserved");
checkRemote(empty.remoteAddress, firstPeer, "empty datagram endpoint");

const largestIPv4Payload = new Uint8Array(65507);
for (let index = 0; index < largestIPv4Payload.length; index++) largestIPv4Payload[index] = index % 251;
const largeReceive = receiver.receive();
if (await secondPeer.send(largestIPv4Payload, receiver.address) !== largestIPv4Payload.byteLength) throw Error("large datagram send count");
const large = await largeReceive;
check(large.data.byteLength === largestIPv4Payload.byteLength, "maximum IPv4 UDP payload was truncated");
for (let index = 0; index < largestIPv4Payload.length; index++) {
  if (large.data[index] !== largestIPv4Payload[index]) throw Error("large datagram byte changed at " + index);
}
checkRemote(large.remoteAddress, secondPeer, "large datagram endpoint");

await receiver.close();
await firstPeer.close();
await secondPeer.close();
`
	executeUDPTest(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 6*time.Second)
}

func TestUDPValidationPrecedesBindAndBindSignalStopsOwningAfterReturn(t *testing.T) {
	occupied, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.LocalAddr().(*net.UDPAddr).Port
	code := fmt.Sprintf(`
import {bind} from "noe:sockets";
function check(condition, message) { if (!condition) throw Error(message); }
async function expectError(label, operation, name) {
  let value, failure;
  try { value = await operation(); } catch (error) { failure = error; }
  if (value && typeof value.close === "function") await value.close();
  if (!failure || failure.name !== name) throw Error(label + " error=" + failure);
}
async function expectReason(label, operation, reason) {
  let value, failure;
  try { value = await operation(); } catch (error) { failure = error; }
  if (value && typeof value.close === "function") await value.close();
  if (failure !== reason) throw Error(label + " did not preserve the exact abort reason");
}

const host = "127.0.0.1", occupiedPort = %d;
await expectError("missing host", () => bind({network:"udp", port:occupiedPort}), "TypeError");
await expectError("empty host", () => bind({network:"udp", host:"", port:occupiedPort}), "TypeError");
await expectError("wrong network", () => bind({network:"tcp", host, port:occupiedPort}), "TypeError");
await expectError("negative port", () => bind({network:"udp", host, port:-1}), "RangeError");
await expectError("large port", () => bind({network:"udp", host, port:65536}), "RangeError");
await expectError("string port", () => bind({network:"udp", host, port:String(occupiedPort)}), "RangeError");
await expectError("TLS certificate field", () => bind({network:"udp", host, port:occupiedPort, cert:"UDP_CERT_SECRET"}), "TypeError");
await expectError("TLS key field", () => bind({network:"udp", host, port:occupiedPort, key:"UDP_KEY_SECRET"}), "TypeError");
await expectError("TLS CA field", () => bind({network:"udp", host, port:occupiedPort, ca:"UDP_CA_SECRET"}), "TypeError");
await expectError("TLS serverName field", () => bind({network:"udp", host, port:occupiedPort, serverName:"localhost"}), "TypeError");

const bindReason = {kind:"pre-aborted bind"};
const preAborted = new AbortController();
preAborted.abort(bindReason);
await expectReason("pre-aborted bind", () => bind({network:"udp", host, port:occupiedPort, signal:preAborted.signal}), bindReason);

const socket = await bind({network:"udp", host, port:0});
await expectError("non-Uint8Array send", () => socket.send("not bytes", socket.address), "TypeError");
await expectError("zero destination port", () => socket.send(new Uint8Array(0), {network:"udp", host, port:0}), "RangeError");
await expectError("wrong destination network", () => socket.send(new Uint8Array(0), {network:"tcp", host, port:occupiedPort}), "TypeError");

const peer = await bind({network:"udp", host, port:0});
const bindingSignal = new AbortController();
const owned = await bind({network:"udp", host, port:0, signal:bindingSignal.signal});
bindingSignal.abort({kind:"abort after bind"});
const pending = owned.receive();
if (await peer.send(new Uint8Array([7, 0, 9]), owned.address) !== 3) throw Error("post-bind send count");
const packet = await pending;
if (Array.from(packet.data).join(",") !== "7,0,9") throw Error("bind signal canceled an owned socket");

await socket.close();
await peer.close();
await owned.close();
`, port)
	executeUDPTest(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 6*time.Second)
}

func TestUDPOperationAbortIsScopedAndSocketRemainsReusable(t *testing.T) {
	code := `
import {bind} from "noe:sockets";
function check(condition, message) { if (!condition) throw Error(message); }
function settled(promise) {
  return promise.then(value => ({value}), error => ({error}));
}

const receiver = await bind({network:"udp", host:"127.0.0.1", port:0});
const peer = await bind({network:"udp", host:"127.0.0.1", port:0});

const queuedController = new AbortController();
const activeRead = receiver.receive();
const queuedRead = settled(receiver.receive({signal:queuedController.signal}));
const queuedReason = {kind:"cancel queued receive"};
queuedController.abort(queuedReason);
const queuedOutcome = await queuedRead;
check(queuedOutcome.error === queuedReason, "queued receive lost its exact abort reason");
if (await peer.send(new Uint8Array([11]), receiver.address) !== 1) throw Error("queued-cancel send count");
const firstPacket = await activeRead;
check(firstPacket.data[0] === 11, "queued cancellation disturbed the active receive");

const activeController = new AbortController();
const activeReadOutcome = settled(receiver.receive({signal:activeController.signal}));
const nextRead = receiver.receive();
const activeReason = {kind:"cancel active receive"};
activeController.abort(activeReason);
const activeOutcome = await activeReadOutcome;
check(activeOutcome.error === activeReason, "active receive lost its exact abort reason");
if (await peer.send(new Uint8Array([12, 0, 13]), receiver.address) !== 3) throw Error("active-cancel send count");
const nextPacket = await nextRead;
check(Array.from(nextPacket.data).join(",") === "12,0,13", "active cancellation poisoned the next receive");

const sendController = new AbortController();
const sendReason = {kind:"pre-aborted send"};
sendController.abort(sendReason);
const canceledSend = await settled(peer.send(new Uint8Array([99]), receiver.address, {signal:sendController.signal}));
check(canceledSend.error === sendReason, "send lost its exact abort reason");
const finalRead = receiver.receive();
if (await peer.send(new Uint8Array([14]), receiver.address) !== 1) throw Error("reuse send count");
const finalPacket = await finalRead;
check(finalPacket.data.length === 1 && finalPacket.data[0] === 14, "socket did not remain reusable after send cancellation");

await receiver.close();
await peer.close();
`
	executeUDPTest(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 6*time.Second)
}

func TestUDPCloseIsIdempotentRejectsPendingReceiveAndReleasesPort(t *testing.T) {
	code := `
import {bind} from "noe:sockets";
function check(condition, message) { if (!condition) throw Error(message); }
const socket = await bind({network:"udp", host:"127.0.0.1", port:0});
const port = socket.address.port;
const pending = socket.receive().then(() => ({resolved:true}), error => ({error}));
const disposed = socket[Symbol.asyncDispose]();
const firstClose = socket.close();
const secondClose = socket.close();
check(firstClose === secondClose, "repeated close did not return the same Promise");
const outcome = await pending;
check(outcome.error !== undefined, "async disposal left a pending receive unresolved");
check(await firstClose === undefined, "close did not resolve undefined");
check(await disposed === undefined, "async disposal did not resolve undefined");

const replacement = await bind({network:"udp", host:"127.0.0.1", port});
check(replacement.address.port === port, "closed UDP port could not be rebound");
await replacement.close();
`
	executeUDPTest(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 6*time.Second)
}

func TestUDPWorkerTerminationReclaimsSocketWithPendingReceive(t *testing.T) {
	workerSource := `
import {parentPort} from "node:worker_threads";
import {bind} from "noe:sockets";
const socket = await bind({network:"udp", host:"127.0.0.1", port:0});
parentPort.postMessage({kind:"bound", port:socket.address.port});
const pending = socket.receive();
parentPort.postMessage({kind:"receiving"});
pending.catch(() => {});
await new Promise(() => {});
`
	code := fmt.Sprintf(`
import {Worker} from "node:worker_threads";
import {bind} from "noe:sockets";
const worker = new Worker(%q, {eval:true, type:"module"});
function nextMessage() {
  return new Promise((resolve, reject) => {
    worker.once("message", resolve);
    worker.once("error", reject);
  });
}
const boundPromise = nextMessage();
const bound = await boundPromise;
if (bound.kind !== "bound" || !Number.isInteger(bound.port) || bound.port < 1) throw Error("Worker did not bind UDP");
const receivingPromise = nextMessage();
const receiving = await receivingPromise;
if (receiving.kind !== "receiving") throw Error("Worker did not start its receive");
await worker.terminate();
const replacement = await bind({network:"udp", host:"127.0.0.1", port:bound.port});
await replacement.close();
`, workerSource)
	executeUDPTest(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true, Workers: &stdlib.WorkerOptions{Max: 1}}}, 8*time.Second)
}

func TestUDPRuntimeCancellationReclaimsSocketWithPendingReceive(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	boundPort := make(chan int, 1)
	profile := stdlib.Profile{
		Modules: stdlib.Modules{RawSockets: true},
		Initialize: func(_ context.Context, bindings *stdlib.Bindings) (func(), error) {
			return nil, bindings.Runtime().Set("notifyBoundPort", func(port int) { boundPort <- port })
		},
	}
	done := make(chan struct{})
	var result stdlib.Result
	var executionErr error
	go func() {
		result, executionErr = stdlib.Execute(ctx, stdlib.Source{Name: "[UDP runtime cancellation]", Virtual: true, Module: true, Code: `
import {bind} from "noe:sockets";
const socket = await bind({network:"udp", host:"127.0.0.1", port:0});
notifyBoundPort(socket.address.port);
await socket.receive();
`}, profile)
		close(done)
	}()
	var port int
	select {
	case port = <-boundPort:
	case <-ctx.Done():
		t.Fatal("runtime ended before UDP bind completed")
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("runtime did not bind UDP")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runtime cancellation did not interrupt the pending UDP receive")
	}
	if !errors.Is(executionErr, context.Canceled) {
		t.Fatalf("runtime cancellation error=%v result=%+v", executionErr, result)
	}
	rebind := fmt.Sprintf(`import {bind} from "noe:sockets";const socket=await bind({network:"udp",host:"127.0.0.1",port:%d});await socket.close();`, port)
	executeUDPTest(t, rebind, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 4*time.Second)
}

func TestUDPGrantDoesNotSelectOtherModulesOrUseRawSocketsTLSAsGrant(t *testing.T) {
	code := `
import {bind} from "noe:sockets";
if (typeof fetch !== "undefined") throw Error("RawSockets selected Fetch");
if (typeof WebSocket !== "undefined") throw Error("RawSockets selected WebSockets");
async function absent(specifier) {
  let missing = false;
  try { await import(specifier); } catch { missing = true; }
  if (!missing) throw Error(specifier + " was selected implicitly");
}

await absent("noe:fs");
const socket = await bind({network:"udp", host:"127.0.0.1", port:0});
await socket.close();
`
	executeUDPTest(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 4*time.Second)

	noGrant := `
async function absent(specifier) {
  let missing = false;
  try { await import(specifier); } catch { missing = true; }
  if (!missing) throw Error(specifier + " was selected without RawSockets");
}
await absent("noe:sockets");
`
	executeUDPTest(t, noGrant, stdlib.Profile{Modules: stdlib.Modules{RawSocketsTLS: &stdlib.SocketTLSOptions{}}}, 4*time.Second)
}

func TestUDPHostnameDestinationUsesCompatibleIPv4Address(t *testing.T) {
	ips, err := net.DefaultResolver.LookupIPAddr(t.Context(), "localhost")
	if err != nil {
		t.Skipf("localhost resolution unavailable: %v", err)
	}
	var ipv4 bool
	for _, ip := range ips {
		ipv4 = ipv4 || ip.IP.To4() != nil
	}
	if !ipv4 {
		t.Skip("localhost has no IPv4 address on this host")
	}
	executeUDPTest(t, `
import {bind} from "noe:sockets";
await using receiver=await bind({network:"udp",host:"127.0.0.1",port:0});
await using sender=await bind({network:"udp",host:"127.0.0.1",port:0});
const packet=receiver.receive();
await sender.send(new Uint8Array([4]),{...receiver.address,host:"localhost"});
if((await packet).data[0]!==4)throw Error("hostname resolution selected an incompatible UDP family");
`, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 4*time.Second)
}

func TestUDPIPv6LoopbackPreservesEndpoints(t *testing.T) {
	probe, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("::1")})
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	_ = probe.Close()
	executeUDPTest(t, `
import {bind} from "noe:sockets";
await using receiver=await bind({network:"udp",host:"::1",port:0});
await using sender=await bind({network:"udp",host:"::1",port:0});
const packet=receiver.receive();
await sender.send(new Uint8Array([6,0,255]),receiver.address);
const value=await packet;
if(value.data.join()!=="6,0,255"||value.remoteAddress.host!=="::1"||value.remoteAddress.port!==sender.address.port||value.remoteAddress.network!=="udp")throw Error("IPv6 UDP bytes or source address changed");
`, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 4*time.Second)
}
