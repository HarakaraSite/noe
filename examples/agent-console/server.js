import * as agents from "noe:agent";
import { serve } from "noe:http";
import { readFile } from "node:fs/promises";
import { extname, resolve, sep } from "node:path";
import { AgentController, HTTPError } from "./controller.js";
import { HistoryQueryError, SessionStore } from "./store.js";

const responseHeaders = {
  "cache-control": "no-store",
  "x-content-type-options": "nosniff",
  "content-security-policy": "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'",
};
const contentTypes = { ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8" };
const json = (body, status = 200) => Response.json(body, { status, headers: responseHeaders });

function cursor(params, name) {
  if (!params.has(name)) return undefined;
  const raw = params.get(name);
  if (!/^\d+$/.test(raw)) throw new HTTPError(400, "Invalid " + name);
  const value = Number(raw);
  if (!Number.isSafeInteger(value)) throw new HTTPError(400, "Invalid " + name);
  return value;
}

export async function startConsole({ config, dataDir, publicDir, port = 8787, pollInterval = 1000, api = agents }) {
  const worker = await api.spawn_agent(config, undefined, { keepAlive: true });
  let controller;
  let server;
  try {
    const store = await SessionStore.create(dataDir, {
      agentId: worker.agentId,
      name: config.name,
      model: { provider: config.provider, model: config.model, effort: config.effort },
    });
    controller = new AgentController(api, store, config, worker.agentId, pollInterval);
    await controller.start();
    let origin = "";
    let authority = "";
    const root = resolve(publicDir);
    server = serve({ hostname: "127.0.0.1", port }, async request => {
      try {
        const url = new URL(request.url);
        // NOE serve projects the incoming authority into Request.url; net/http's
        // Host is not an ordinary header. Compare it with the listener, never
        // derive the ALLOWED authority/origin from this untrusted request.
        const hostHeader = request.headers.get("host");
        if (url.origin !== origin || (hostHeader !== null && hostHeader !== authority)) {
          throw new HTTPError(403, "Host is not allowed");
        }
        if (request.method === "GET") {
          if (url.pathname === "/api/state") return json(controller.state());
          if (url.pathname === "/api/sessions") return json(await store.sessions());
          if (url.pathname === "/api/history") {
            return json(await store.history({
              sessionId: url.searchParams.get("session") ?? store.sessionId,
              before: cursor(url.searchParams, "before"),
              after: cursor(url.searchParams, "after"),
              limit: cursor(url.searchParams, "limit") ?? 100,
            }));
          }
          if (url.pathname.startsWith("/api/")) throw new HTTPError(404, "Not found");
          let pathname;
          try { pathname = decodeURIComponent(url.pathname); }
          catch { throw new HTTPError(400, "Invalid path"); }
          const file = resolve(root, "." + (pathname === "/" ? "/index.html" : pathname));
          if (!file.startsWith(root + sep) || !contentTypes[extname(file)]) throw new HTTPError(404, "Not found");
          return new Response(await readFile(file, "utf8"), { headers: { ...responseHeaders, "content-type": contentTypes[extname(file)] } });
        }
        const match = /^\/api\/(queue|steer|interrupt|stop)$/.exec(url.pathname);
        if (!match) throw new HTTPError(404, "Not found");
        if (request.method !== "POST") throw new HTTPError(405, "Use POST");
        if (request.headers.get("origin") !== origin) throw new HTTPError(403, "Origin is not allowed");
        if (request.headers.get("content-type")?.split(";")[0].trim().toLowerCase() !== "application/json") {
          throw new HTTPError(415, "Use application/json");
        }
        let body;
        try { body = await request.json(); }
        catch { throw new HTTPError(400, "Invalid JSON"); }
        if (!body || typeof body !== "object" || Array.isArray(body)) throw new HTTPError(400, "Use a JSON object");
        return json(await controller.operation(match[1], body));
      } catch (error) {
        const status = error instanceof HTTPError ? error.status :
          error instanceof HistoryQueryError ? 400 : error.code === "ENOENT" ? 404 : 500;
        if (status === 500) console.error("Agent console HTTP:", error);
        return json({ error: error.message ?? String(error), ...(error.record ? { record: error.record } : {}) }, status);
      }
    });
    const address = new URL(server.url);
    origin = address.origin;
    authority = address.host;
    return { server, controller, store };
  } catch (error) {
    controller?.close();
    server?.close();
    await api.stop_agent({ agentId: worker.agentId });
    throw error;
  }
}
