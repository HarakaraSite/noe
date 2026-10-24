import { spawn } from "noe:process";
import { stdout, stderr } from "noe:io";
import { randomUUID } from "node:crypto";
import { dirname, resolve } from "node:path";

const message = error => String(error?.message || error);
const json = (value, status = 200, extra = {}) => Response.json(value, {
  status, headers: { "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", ...extra },
});

export async function startAgentWrapper(options, html) {
  let child = null, worker = null, childStatus = null, closing = false;
  let history = "", diagnostics = "", outputError = null, closePromise = null;
  let resolveReady;
  const ready = new Promise(resolve => { resolveReady = resolve; });
  const readyPath = "/worker-ready/" + randomUUID();
  let bootstrapSent = false, diagnosticLines = "";
  let outputTasks = [];
  const out = stdout.getWriter(), err = stderr.getWriter();
  function allowed(request) {
    const hosts = ["127.0.0.1:" + server.port, "localhost:" + server.port];
    const host = new URL(request.url).host;
    const origin = request.headers.get("origin");
    return hosts.includes(host) && (!origin || hosts.some(value => origin === "http://" + value)) &&
      request.headers.get("sec-fetch-site") !== "cross-site";
  }
  function method(request, methods) {
    return methods.includes(request.method) ? null : json({ error: "Method not allowed; use " + methods.join(", ") }, 405, { Allow: methods.join(", ") });
  }
  async function proxy(request, path) {
    if (!worker || childStatus || closing) return json({ error: "Agent server unavailable" }, 503);
    const headers = new Headers();
    const type = request.headers.get("content-type");
    if (type) headers.set("Content-Type", type);
    const response = await fetch(worker.url.replace(/\/$/, "") + path, {
      method: request.method, headers,
      ...(["GET", "HEAD"].includes(request.method) ? {} : { body: await request.text() }),
    });
    return new Response(await response.text(), { status: response.status, headers: response.headers });
  }
  async function close() {
    if (closePromise) return closePromise;
    closing = true;
    closePromise = (async () => {
      try {
        if (worker && !childStatus) {
          const response = await fetch(worker.url.replace(/\/$/, "") + "/shutdown", { method: "POST" });
          if (!response.ok) throw new Error("Worker shutdown failed: HTTP " + response.status);
        }
        if (child) {
          if (!worker && !childStatus) await child.kill("SIGINT");
          await child.exited;
          await Promise.all(outputTasks);
          await child.close();
        }
      } finally {
        if (child) await child.close();
        server.close();
        out.releaseLock(); err.releaseLock();
      }
    })();
    return closePromise;
  }
  const server = serve({ hostname: "127.0.0.1", port: options.port }, async request => {
    try {
      if (!allowed(request)) return json({ error: "Cross-origin requests or unexpected Host are not allowed" }, 403);
      const path = new URL(request.url).pathname;
      if (path === readyPath) {
        const invalid = method(request, ["POST"]); if (invalid) return invalid;
        if (worker || closing) return json({ error: "Worker already registered or closing" }, 409);
        const value = await request.json();
        const url = new URL(value.url);
        if (url.protocol !== "http:" || url.hostname !== "127.0.0.1" || !url.port || !value.agentId) {
          return json({ error: "Invalid worker registration" }, 400);
        }
        worker = value;
        resolveReady(value);
        return json({ status: "accepted" }, 202);
      }
      if (path === "/") {
        const invalid = method(request, ["GET"]); if (invalid) return invalid;
        return new Response(html, { headers: {
          "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff",
          "Content-Security-Policy": "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'",
        } });
      }
      if (path === "/history") {
        const invalid = method(request, ["GET"]); if (invalid) return invalid;
        return json({ available: true, mode: "observe", text: history, stderr: diagnostics,
          error: outputError, workerStatus: childStatus });
      }
      if (path === "/shutdown") {
        const invalid = method(request, ["POST"]); if (invalid) return invalid;
        if (closing) return json({ error: "Server is already shutting down" }, 409);
        closing = true;
        setTimeout(() => { close().catch(error => { console.error(message(error)); process.exit(1); }); }, 25);
        return json({ status: "accepted" }, 202);
      }
      if (path === "/tasks" || /^\/tasks\/[^/]+(?:\/(steer|interrupt))?$/.test(path)) return await proxy(request, path);
      return json({ error: "Not found" }, 404);
    } catch (error) { return json({ error: message(error) }, 502); }
  });
  async function bootstrap(eventsPath) {
    const writer = child.stdin.getWriter();
    try {
      await writer.write(new TextEncoder().encode(JSON.stringify({
        options: { ...options, eventsPath }, readyURL: server.url.replace(/\/$/, "") + readyPath,
      })));
      await writer.close();
    } finally { writer.releaseLock(); }
  }
  async function capture(stream, destination, isError) {
    const reader = stream.getReader(), decoder = new TextDecoder();
    function append(text) {
      if (!isError) { history += text; return; }
      diagnostics += text;
      diagnosticLines += text;
      let end;
      while ((end = diagnosticLines.indexOf("\n")) !== -1) {
        const line = diagnosticLines.slice(0, end).replace(/\r$/, "");
        diagnosticLines = diagnosticLines.slice(end + 1);
        // --events announces its automatically chosen absolute filename before
        // executing worker.js. Use it only for backend steer/progress metadata.
        if (!bootstrapSent && line.startsWith("noe: events: ")) {
          bootstrapSent = true;
          bootstrap(line.slice("noe: events: ".length)).catch(error => {
            outputError = "Worker bootstrap error: " + message(error);
            child.kill("SIGINT").catch(() => {});
          });
        }
      }
    }
    try {
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        append(decoder.decode(value, { stream: true }));
        await destination.write(value);
      }
      append(decoder.decode());
    } catch (error) {
      outputError = "Worker output error: " + message(error);
      if (child) child.kill("SIGINT").catch(() => {});
    } finally { reader.releaseLock(); }
  }
  try {
    const workerPath = resolve(dirname(resolve(process.argv[1])), "worker.js");
    child = await spawn(process.argv[0], ["run", "--observe", "--events", workerPath], {
      stdin: "pipe", stdout: "pipe", stderr: "pipe",
    });
    outputTasks = [capture(child.stdout, out, false), capture(child.stderr, err, true)];
    const exited = child.exited.then(status => {
      childStatus = status;
      return status;
    });
    const startup = await Promise.race([ready, exited.then(status => {
      throw new Error("Worker exited during startup: " + JSON.stringify(status) + "\n" + diagnostics);
    })]);
    // Keep serving captured history after an unexpected worker exit. API
    // mutations fail rather than restarting the conversation behind the UI.
    return { url: server.url, agentId: startup.agentId, close };
  } catch (error) {
    await close();
    throw error;
  }
}
