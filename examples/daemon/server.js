// Serve loopback JSONL requests until SIGTERM, then close the listener and clients.
import { listen } from "noe:sockets";
import { subscribe } from "noe:signals";

const port = Number(process.argv[2] ?? 9070);
if (!Number.isInteger(port) || port < 0 || port > 65535) throw new Error("Usage: server.js [port 0..65535]");
const signals = subscribe("SIGTERM");
const connections = new Set();
const tasks = new Set();
const encoder = new TextEncoder();
let listener;
let stopping = false;
let stopPromise;

function stop(reason) {
  if (!stopPromise) {
    stopping = true;
    console.log(JSON.stringify({ event: "stopping", reason }));
    stopPromise = Promise.all([
      ...(listener ? [listener.close()] : []),
      ...Array.from(connections, connection => connection.close()),
    ]);
  }
  return stopPromise;
}

function respond(line) {
  let request;
  try {
    request = JSON.parse(line);
    if (!request || typeof request !== "object") throw new Error("Request must be an object");
    if (request.op === "ping") return { id: request.id ?? null, ok: true, result: { pid: process.pid } };
    if (request.op === "echo" && typeof request.text === "string") {
      return { id: request.id ?? null, ok: true, result: request.text };
    }
    if (request.op === "sum" && Array.isArray(request.values) && request.values.every(Number.isFinite)) {
      return { id: request.id ?? null, ok: true, result: request.values.reduce((a, b) => a + b, 0) };
    }
    throw new Error("Expected ping, echo with text, or sum with finite values");
  } catch (error) {
    return { id: request?.id ?? null, ok: false, error: String(error.message || error) };
  }
}

async function handle(connection) {
  const writer = connection.writable.getWriter();
  const decoder = new TextDecoder();
  let pending = "";
  const send = async line => { await writer.write(encoder.encode(JSON.stringify(respond(line)) + "\n")); };
  try {
    for await (const chunk of connection.readable) {
      pending += decoder.decode(chunk, { stream: true });
      let end;
      while ((end = pending.indexOf("\n")) >= 0) {
        const line = pending.slice(0, end);
        pending = pending.slice(end + 1);
        if (line.trim()) await send(line);
      }
    }
    pending += decoder.decode();
    if (pending.trim()) await send(pending);
    await writer.close();
  } catch (error) {
    if (!stopping) console.error("client:", String(error.message || error));
  } finally {
    writer.releaseLock();
    try { await connection.close(); }
    finally { connections.delete(connection); }
  }
}

let signalTask;
try {
  listener = await listen({ network: "tcp", host: "127.0.0.1", port });
  signalTask = signals.next().then(async notification => {
    if (!notification.done) await stop(notification.value);
  });
  console.log(JSON.stringify({ event: "ready", pid: process.pid, address: listener.address }));
  while (!stopping) {
    let connection;
    try { connection = await listener.accept(); }
    catch (error) { if (stopping) break; throw error; }
    if (stopping) { await connection.close(); break; }
    connections.add(connection);
    const task = handle(connection);
    tasks.add(task);
    task.finally(() => tasks.delete(task)).catch(error => console.error("client cleanup:", String(error)));
  }
} finally {
  try { await stop("cleanup"); }
  finally {
    await signals.close();
    await Promise.allSettled(tasks);
    if (signalTask) await signalTask;
  }
}
console.log(JSON.stringify({ event: "stopped", connections: connections.size }));
