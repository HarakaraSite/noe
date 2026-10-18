// Reply to JSONL requests as they arrive, without waiting for stdin EOF.
import { stdin, stdout } from "noe:io";

const decoder = new TextDecoder();
const encoder = new TextEncoder();
const writer = stdout.getWriter();
let pending = "";
async function reply(line) {
  if (!line.trim()) return;
  const request = JSON.parse(line);
  await writer.write(encoder.encode(JSON.stringify({ id: request.id, result: request.value }) + "\n"));
}
try {
  for await (const bytes of stdin) {
    pending += decoder.decode(bytes, { stream: true });
    let at;
    while ((at = pending.indexOf("\n")) >= 0) {
      const line = pending.slice(0, at);
      pending = pending.slice(at + 1);
      await reply(line);
    }
  }
  pending += decoder.decode();
  await reply(pending);
} finally {
  writer.releaseLock();
}
