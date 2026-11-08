package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestWebCryptoResizableBufferSources(t *testing.T) {
	const source = `
async function checkBuffers() {
  async function rejects(operation) {
    const pending = operation();
    if (!(pending instanceof Promise)) throw new Error("expected a rejected Promise");
    try { await pending; } catch (error) {
      if (error instanceof TypeError) return;
      throw error;
    }
    throw new Error("resizable BufferSource was accepted");
  }
  const buffer = new ArrayBuffer(8, {maxByteLength: 16});
  const outOfBounds = new DataView(buffer, 4, 4);
  buffer.resize(2);
  // Conversion must use the backing buffer's intrinsic state.
  Object.defineProperty(buffer, "resizable", {value: false});
  for (const input of [buffer, new Uint8Array(buffer), new DataView(buffer), outOfBounds]) {
    await rejects(() => crypto.subtle.digest("SHA-256", input));
  }
  await rejects(() => crypto.subtle.importKey("raw", buffer,
    {name: "HMAC", hash: "SHA-256"}, false, ["sign"]));
  const key = await crypto.subtle.importKey("raw", new Uint8Array([1]), "PBKDF2", false, ["deriveBits"]);
  await rejects(() => crypto.subtle.deriveBits(
    {name: "PBKDF2", salt: outOfBounds, iterations: 1, hash: "SHA-256"}, key, 8));

  const hex = value => Array.from(new Uint8Array(value), b => b.toString(16).padStart(2, "0")).join("");
  const empty = await crypto.subtle.digest("SHA-256", new DataView(new ArrayBuffer(0)));
  if (hex(empty) !== "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855") {
    throw new Error("valid empty DataView changed");
  }
  const bytes = new Uint8Array([0, 97, 98, 99, 0]);
  const span = await crypto.subtle.digest("SHA-256", new DataView(bytes.buffer, 1, 3));
  if (hex(span) !== "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad") {
    throw new Error("DataView byte span changed");
  }
  return "ok";
}
`
	worker := `import {parentPort} from "node:worker_threads";` + source + `
checkBuffers().then(result => { parentPort.postMessage(result); parentPort.close(); });`
	out, errOut := run(t, stdlib.Config{
		Workers: workerFiles(map[string]string{"./crypto-buffers.mjs": worker}),
	}, source+`
checkBuffers().then(result => {
  console.log("main", result);
  const worker = new require_worker_threads.Worker("./crypto-buffers.mjs");
  worker.on("message", result => console.log("worker", result));
});`)
	if out != "main ok\nworker ok" || errOut != "" {
		t.Fatalf("BufferSource results: stdout=%q stderr=%q", out, errOut)
	}
}
