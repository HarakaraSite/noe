import { spawn_agent, queue_agent, observe_agent, steer_agent, interrupt_agent } from "noe:agent";
import { readFile, stat } from "node:fs/promises";
import { setTimeout as sleep } from "node:timers/promises";

const terminal = new Set(["completed", "failed", "cancelled"]);
const message = error => String(error?.message || error);
const now = () => new Date().toISOString();
class HTTPError extends Error {
  constructor(status, text) { super(text); this.status = status; }
}
const json = (value, status = 200, extra = {}) => Response.json(value, {
  status, headers: { "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", ...extra },
});

// Exactly one task is ever handed to NOE at once. Saving lastResult and
// releasing the next reservation happen under the same promise-based lock.
export async function startAgentServer(options, html) {
  const config = { name: "http-worker", provider: options.provider, model: options.model, effort: options.effort };
  const tasks = new Map();
  const prefix = "http-" + Date.now() + "-";
  let serial = Promise.resolve();
  function locked(fn) {
    const result = serial.then(fn);
    serial = result.catch(() => {});
    return result;
  }
  let sequence = 0, steerSequence = 0, active = null, timer = null;
  let closing = false, closePromise = null, supervisorError = null, logError = null;
  let snapshot = null, server = null, logOffset = 0, executionId = null;
  // Logs are optional: live instance snapshots always provide task results.
  // The current log is used only for extra progress and exact steer lifecycle.
  if (options.eventsPath) {
    await stat(options.eventsPath);
    const initial = await readFile(options.eventsPath, "utf8");
    for (const line of initial.split("\n")) {
      try { const event = JSON.parse(line); if (event.type === "execution.started") executionId = event.executionId; } catch {}
    }
    if (!executionId) throw new Error("--events-path must be this NOE invocation's --events JSONL file");
  }
  const { agentId } = await spawn_agent(config, undefined, { keepAlive: true });
  const view = task => JSON.parse(JSON.stringify(task));
  const unavailable = () => snapshot && ["stopping", "stopped", "failed"].includes(snapshot.state);
  function finish(task, result) {
    task.runId = result.runId || task.runId;
    task.result = { ...result };
    task.state = result.state;
    task.answer = result.answer || "";
    task.error = result.error || null;
    task.finishedAt = now();
    for (const steer of task.steers) {
      if (!["applied", "cancelled"].includes(steer.status)) {
        // NOE commits a successful answer only after applying accepted steers.
        // Failure/interrupt alone does NOT prove whether a steer was applied.
        steer.status = result.state === "completed" ? "applied" : "unknown";
        steer.confirmation = result.state === "completed" ? "final_commit" : "unconfirmed";
      }
    }
  }
  async function readEvents() {
    if (!options.eventsPath) return;
    try {
      const text = await readFile(options.eventsPath, "utf8");
      if (text.length < logOffset) throw new Error("Event log was truncated; do not rotate it while serving");
      const end = text.lastIndexOf("\n") + 1;
      const lines = text.slice(logOffset, end).split("\n");
      for (const line of lines) {
        if (!line) continue;
        const event = JSON.parse(line);
        if (event.executionId !== executionId || event.operation !== "steer") continue;
        if (!["command.applied", "command.cancelled"].includes(event.type)) continue;
        for (const task of tasks.values()) {
          if (task.runId !== event.runId) continue;
          const steer = task.steers.find(item => item.commandId === event.commandId);
          if (steer) {
            steer.status = event.status || event.type.slice("command.".length);
            steer.confirmation = "event";
          }
        }
      }
      logOffset = end;
      logError = null;
    } catch (error) { logError = message(error); }
  }
  async function progress(task) {
    if (!options.eventsPath || !task.runId) return;
    try {
      const observed = await observe_agent({ logPath: options.eventsPath, runId: task.runId });
      task.progress = {
        status: observed.status, phase: observed.phase, action: observed.action,
        lastMessage: observed.lastMessage, lastToolResult: observed.lastToolResult,
        usage: observed.usage, updatedAt: observed.updatedAt,
      };
    } catch (error) { logError = message(error); }
  }
  async function refresh() {
    snapshot = await observe_agent({ agentId });
    supervisorError = null;
    if (active) {
      const task = active;
      if (snapshot.currentRunId && !task.runId) {
        task.runId = snapshot.currentRunId;
        task.startedAt = now();
        if (task.state !== "stopping") task.state = "running";
      }
      const result = snapshot.lastResult;
      if (result?.commandId === task.commandId && !terminal.has(task.state)) finish(task, result);
      await progress(task);
      // lastResult is retained even if completion happens between two polls.
      // Never submit the next command before persisting this command's result.
      if (terminal.has(task.state) && ["idle", "stopped", "failed"].includes(snapshot.state)) active = null;
    }
    await readEvents();
    if (unavailable()) {
      for (const task of tasks.values()) {
        if (!terminal.has(task.state)) {
          task.state = "failed";
          task.error = "Agent instance unavailable: " + (snapshot.reason || snapshot.state);
          task.finishedAt = now();
        }
      }
      active = null;
    }
  }
  async function dispatch() {
    if (closing || active || snapshot?.state !== "idle") return;
    const task = [...tasks.values()].find(item => item.state === "queued");
    if (!task) return;
    task.state = "starting";
    task.commandId = prefix + task.id;
    active = task;
    try {
      const receipt = await queue_agent({ agentId, instruction: task.instruction, commandId: task.commandId });
      task.commandId = receipt.commandId;
    } catch (error) {
      task.state = "failed";
      task.error = message(error);
      task.finishedAt = now();
      active = null;
    }
  }
  async function poll() {
    try {
      await locked(async () => {
        await refresh();
        if (active && active.runId && !terminal.has(active.state) && active.state !== "stopping") {
          for (const steer of active.steers) {
            if (!["accepted", "delivered"].includes(steer.status)) continue;
            try {
              const receipt = await steer_agent({ runId: active.runId, instruction: steer.instruction, commandId: steer.commandId });
              steer.status = receipt.status;
              if (receipt.status === "applied") steer.confirmation = "receipt";
            } catch {
              // The task may have finished. Only a result/event can confirm it.
            }
          }
        }
        await dispatch();
      });
    } catch (error) { supervisorError = message(error); }
    if (!closing) timer = setTimeout(poll, 100);
  }
  function close() {
    if (closePromise) return closePromise;
    closing = true;
    if (timer !== null) clearTimeout(timer);
    closePromise = (async () => {
      try {
        await locked(async () => {
          for (const task of tasks.values()) {
            if (task.state === "queued") {
              task.state = "cancelled"; task.finishedAt = now();
            }
          }
          await interrupt_agent({ agentId });
        });
        // Interrupt returns on acceptance, NOT on owned-work cleanup.
        while (true) {
          await locked(refresh);
          if (["stopped", "failed"].includes(snapshot.state)) break;
          await sleep(25);
        }
      } finally {
        // Do not invoke close synchronously from its own HTTP handler.
        if (server) server.close();
      }
    })();
    return closePromise;
  }
  async function instruction(request) {
    const type = request.headers.get("content-type") || "";
    if (type.split(";")[0].trim().toLowerCase() !== "application/json") throw new HTTPError(415, "Use Content-Type: application/json");
    let body;
    try { body = await request.json(); } catch { throw new HTTPError(400, "Invalid JSON"); }
    if (!body || typeof body !== "object" || Array.isArray(body) || typeof body.instruction !== "string" || !body.instruction.trim()) {
      throw new HTTPError(400, "instruction must be a nonempty string");
    }
    return body.instruction;
  }
  function allowed(request) {
    // Loopback binding alone does not protect against browser CSRF/rebinding.
    const hosts = ["127.0.0.1:" + server.port, "localhost:" + server.port];
    const host = new URL(request.url).host; // serve puts Go Request.Host in the URL, not Headers.
    if (!hosts.includes(host)) throw new HTTPError(403, "Unexpected Host");
    const origin = request.headers.get("origin");
    if (origin && !hosts.some(value => origin === "http://" + value)) throw new HTTPError(403, "Cross-origin requests are not allowed");
    if (request.headers.get("sec-fetch-site") === "cross-site") throw new HTTPError(403, "Cross-site requests are not allowed");
  }
  function method(request, methods) {
    if (!methods.includes(request.method)) {
      const error = new HTTPError(405, "Method not allowed; use " + methods.join(", "));
      error.allow = methods.join(", ");
      throw error;
    }
  }
  async function handle(request) {
    try {
      allowed(request);
      const path = new URL(request.url).pathname;
      if (path === "/") {
        method(request, ["GET"]);
        return new Response(html, { headers: {
          "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store",
          "X-Content-Type-Options": "nosniff",
          "Content-Security-Policy": "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'",
        } });
      }
      if (path === "/shutdown") {
        method(request, ["POST"]);
        if (closing) throw new HTTPError(409, "Server is already shutting down");
        closing = true;
        setTimeout(() => { close().catch(error => { console.error(message(error)); process.exit(1); }); }, 25);
        return json({ status: "accepted" }, 202);
      }
      if (path === "/tasks") {
        method(request, ["GET", "POST"]);
        if (request.method === "GET") return locked(() => json({
          tasks: [...tasks.values()].map(view),
          agent: snapshot ? { agentId, state: snapshot.state, currentRunId: snapshot.currentRunId, context: snapshot.context, execution: snapshot.execution } : { agentId, state: "starting" },
          supervisorError, logError, closing,
        }));
        const text = await instruction(request);
        return locked(() => {
          if (closing || unavailable()) throw new HTTPError(503, "Agent server unavailable");
          const task = { id: "task-" + (++sequence), instruction: text, state: "queued", runId: null,
            commandId: null, answer: null, error: null, result: null, steers: [], progress: null,
            createdAt: now(), startedAt: null, finishedAt: null };
          tasks.set(task.id, task);
          return json(view(task), 202, { Location: "/tasks/" + task.id });
        });
      }
      const match = /^\/tasks\/([^/]+)(?:\/(steer|interrupt))?$/.exec(path);
      if (!match) throw new HTTPError(404, "Not found");
      const task = tasks.get(match[1]);
      if (!task) throw new HTTPError(404, "Unknown task ID");
      if (!match[2]) {
        method(request, ["GET"]);
        return locked(() => json(view(task)));
      }
      method(request, ["POST"]);
      const text = match[2] === "steer" ? await instruction(request) : null;
      return locked(async () => {
        if (closing) throw new HTTPError(503, "Server is shutting down");
        await refresh();
        if (match[2] === "interrupt" && task.state === "queued") {
          task.state = "cancelled"; task.finishedAt = now();
          return json(view(task), 202);
        }
        if (active !== task || task.state !== "running" || !task.runId) throw new HTTPError(409, "Task is not running (or stop already requested)");
        if (match[2] === "steer") {
          const commandId = prefix + "steer-" + (++steerSequence);
          let receipt;
          try { receipt = await steer_agent({ runId: task.runId, instruction: text, commandId }); }
          catch (error) { throw new HTTPError(409, message(error)); }
          const steer = { ...receipt, instruction: text, acceptedAt: now(), confirmation: "receipt" };
          task.steers.push(steer);
          return json(steer, 202);
        }
        try { await interrupt_agent({ runId: task.runId }); }
        catch (error) { throw new HTTPError(409, message(error)); }
        task.state = "stopping";
        return json({ id: task.id, runId: task.runId, state: task.state, status: "accepted" }, 202);
      });
    } catch (error) { return json({ error: message(error) }, error.status || 500, error.allow ? { Allow: error.allow } : {}); }
  }
  try {
    snapshot = await observe_agent({ agentId });
    server = serve({ hostname: "127.0.0.1", port: options.port }, async request => {
      // Await here so rejected async routes become HTTP errors, not rejections.
      try { return await handle(request); }
      catch (error) { return json({ error: message(error) }, error.status || 500, error.allow ? { Allow: error.allow } : {}); }
    });
    timer = setTimeout(poll, 0);
    return { url: server.url, agentId, close };
  } catch (error) {
    await interrupt_agent({ agentId });
    while (!["stopped", "failed"].includes((await observe_agent({ agentId })).state)) await sleep(25);
    throw error;
  }
}
