import { Client } from "mcp/client";
import { NoeTransport } from "mcp/transport";

async function rejectedWithin(promise, label) {
  let timer;
  try {
    const outcome = await Promise.race([
      promise.then(value => ({ value }), error => ({ error })),
      new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(`${label} left an RPC pending`)), 2000); }),
    ]);
    if (!outcome.error) throw new Error(`${label} unexpectedly succeeded`);
  } finally {
    clearTimeout(timer);
  }
}

async function completeWithin(promise, label) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(`${label} did not settle`)), 3000); }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

async function expectFailure(config, beforeRPC, label) {
  const client = new Client({ name: "noe-process-failure-check", version: "1" });
  const transport = new NoeTransport(config);
  let errors = 0;
  let closes = 0;
  transport.onerror = () => { errors++; };
  transport.onclose = () => { closes++; };
  try {
    if (beforeRPC) {
      await rejectedWithin(client.connect(transport), `${label} spawn failure`);
    } else {
      await client.connect(transport);
      await rejectedWithin(client.listTools(), `${label} server failure`);
    }
  } finally {
    await client.close();
    await transport.close();
  }
  if (errors !== 1 || closes !== 1 || !transport.closed || transport.child !== undefined) {
    throw new Error(`failure cleanup mismatch: errors=${errors} closes=${closes} closed=${transport.closed} child=${!!transport.child}`);
  }
}

const { command, cwd, server } = JSON.parse(process.argv[2]);
await expectFailure({ command: `${command}.does-not-exist`, args: [], cwd }, true, "startup");
for (const mode of ["eof", "malformed"]) {
  await expectFailure({ command, args: ["run", "--timeout", "20s", server, mode], cwd }, false, mode);
}

// Close while spawn is still awaited by start(). Calling connect() starts
// start() synchronously up to its spawn await, so close() enters the exact race.
const startupClient = new Client({ name: "noe-process-start-race", version: "1" });
const startupTransport = new NoeTransport({ command, args: ["run", "--timeout", "20s", server], cwd });
let startupCloses = 0;
startupTransport.onclose = () => { startupCloses++; };
const connecting = startupClient.connect(startupTransport).then(
  () => ({ error: undefined }),
  error => ({ error }),
);
const closing = startupTransport.close();
const [connectResult] = await Promise.all([connecting, closing]);
await startupClient.close();
if (!connectResult.error || startupCloses !== 1 || !startupTransport.closed || startupTransport.child !== undefined) {
  throw new Error(`start/close race did not settle cleanly: error=${!!connectResult.error} closes=${startupCloses} child=${!!startupTransport.child}`);
}

// The fixture acknowledges readiness without ever reading stdin. An 8 MiB
// write is larger than the process pipe, so close must interrupt that pending
// write when its stdin-EOF grace period expires.
const blocked = new NoeTransport({ command, args: ["run", "--timeout", "20s", server, "block"], cwd });
let readyResolve;
const ready = new Promise(resolve => { readyResolve = resolve; });
blocked.onmessage = message => {
  if (message.method === "notifications/ready") readyResolve();
};
await blocked.start();
await completeWithin(ready, "blocker readiness");
let settledWrites = 0;
const blockedWrites = [];
for (let id = 1; id <= 16; id++) {
  blockedWrites.push(blocked.send({
    jsonrpc: "2.0", id, method: "test/blocked-write",
    params: { data: "x".repeat(512 * 1024) },
  }));
}
const writesSettled = Promise.allSettled(blockedWrites);
for (const write of blockedWrites) write.then(() => { settledWrites++; }, () => { settledWrites++; });
await new Promise(resolve => { setTimeout(resolve, 0); });
if (blocked.closed || settledWrites === blockedWrites.length) {
  throw new Error(`blocker did not hold pending writes: settled=${settledWrites}/${blockedWrites.length} exit=${JSON.stringify(blocked.exitStatus)} closed=${blocked.closed}`);
}
await completeWithin(blocked.close(), "blocked-write close");
const settledResults = await completeWithin(writesSettled, "blocked write settlement");
if (!settledResults.some(result => result.status === "rejected")) {
  throw new Error("closing the child did not reject any blocked pipe write");
}
if (!blocked.closed || blocked.child !== undefined || !blocked.exitStatus) {
  throw new Error(`blocked-write child cleanup mismatch: closed=${blocked.closed} child=${!!blocked.child} exit=${JSON.stringify(blocked.exitStatus)}`);
}
console.log(JSON.stringify({ startupFailure: true, eofRejectsPendingRPC: true, malformedRejectsPendingRPC: true, childCleanup: true, startupCloseRace: true, pendingWriteClose: true }));
