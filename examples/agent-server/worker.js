// Internal worker: started by server.js with --observe --events, not directly.
import { stdin } from "noe:io";
import { startAgentServer } from "./app.js";

// Bootstrap travels over stdin, separate from the human-readable stdout.
const { options, readyURL } = JSON.parse(await new Response(stdin).text());
const server = await startAgentServer({ ...options, port: 0 }, "");
try {
  const response = await fetch(readyURL, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url: server.url, agentId: server.agentId }),
  });
  if (!response.ok) throw new Error("Wrapper rejected worker startup: HTTP " + response.status);
} catch (error) {
  await server.close();
  throw error;
}
