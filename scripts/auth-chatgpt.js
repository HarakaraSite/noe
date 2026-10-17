// Register or renew a ChatGPT account for NOE; tokens are never printed.
import { readFileSync, writeFileSync, existsSync, mkdirSync, chmodSync, renameSync, unlinkSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { homedir } from "node:os";
import { randomBytes, createHash } from "node:crypto";
import { save_chatgpt_credentials } from "noe:auth";

const args = process.argv.slice(2);
const command = args[0] || "login";
const configDir = join(process.env.XDG_CONFIG_HOME || join(homedir(), ".config"), "noe");
const file = resolve(args[1] || process.env.NOE_OPENAI_CHATGPT_CREDENTIALS_FILE || join(configDir, "credentials", "openai-chatgpt.json"));
const pendingFile = file + ".pending.json";
const registrationFile = file + ".registration.json";
const resource = "https://api.openai.com/v1";
const issuer = "https://auth.openai.com";

function writeJSON(path, value) {
  mkdirSync(dirname(path), { recursive: true });
  const temp = path + "." + randomBytes(12).toString("hex") + ".tmp";
  writeFileSync(temp, JSON.stringify(value));
  chmodSync(temp, 0o600);
  renameSync(temp, path);
}
function remove(path) { if (existsSync(path)) unlinkSync(path); }
function readJSON(path) { return JSON.parse(readFileSync(path, "utf8")); }
function base64url(text) { return text.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, ""); }
function random() { return base64url(randomBytes(32).toString("base64")); }

if (command === "help" || command === "--help") {
  console.log("Usage: noe run auth-chatgpt.js [login|complete] [credential-file]");
  console.log("login: print a browser URL and wait for the loopback callback.");
  console.log("complete: paste the browser's callback URL on stdin, then send EOF (Ctrl+D).");
} else if (command === "complete") {
  const pending = readJSON(pendingFile);
  const text = process.stdin.read();
  const callback = new URL(String(text || "").trim());
  const expected = new URL(pending.redirectURI);
  if (callback.origin !== expected.origin || callback.pathname !== expected.pathname || callback.hash || callback.username || callback.password) {
    throw new Error("Callback URL does not match the running sign-in attempt");
  }
  const response = await fetch(callback.href);
  if (!response.ok) throw new Error("The running sign-in attempt rejected the callback");
  console.log("Callback delivered. Check the login terminal for the result.");
} else if (command === "login") {
  const saved = existsSync(file) ? readJSON(file) : null;
  const registration = saved || (existsSync(registrationFile) ? readJSON(registrationFile) : null);
  const hostFile = join(configDir, "credentials", "chatgpt-host.json");
  const hostId = registration?.ext_agent_host_id || (existsSync(hostFile) ? readJSON(hostFile).ext_agent_host_id : "urn:uuid:" + crypto.randomUUID());
  if (!existsSync(hostFile)) writeJSON(hostFile, { ext_agent_host_id: hostId });

  const discoveryResponse = await fetch(issuer + "/.well-known/openid-configuration");
  if (!discoveryResponse.ok) throw new Error("OpenAI sign-in discovery failed (HTTP " + discoveryResponse.status + ")");
  const discovery = await discoveryResponse.json();
  if (discovery.issuer !== issuer) throw new Error("OpenAI discovery issuer mismatch");
  const state = random(), nonce = random(), verifier = random();
  const challenge = base64url(createHash("sha256").update(verifier).digest("base64"));
  let accept;
  const callbackPromise = new Promise(resolve => { accept = resolve; });
  let received = false;
  const server = serve({ hostname: "127.0.0.1", port: 0 }, request => {
    const url = new URL(request.url);
    if (url.pathname !== "/auth/callback" || request.method !== "GET") return new Response("Not found", { status: 404 });
    if (url.searchParams.get("state") !== state) return new Response("Invalid sign-in state", { status: 400 });
    if (received) return new Response("Callback already received", { status: 409 });
    received = true;
    accept(url);
    return new Response("Sign-in response received. Return to NOE.\n");
  });
  const redirectURI = "http://127.0.0.1:" + server.port + "/auth/callback";
  try {
    const clientId = registration?.client_id || "dynamic_agent_client";
    const authorize = new URL(discovery.authorization_endpoint);
    const params = {
      client_id: clientId, ext_agent_host_id: hostId, response_type: "code",
      redirect_uri: redirectURI, scope: "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct",
      resource, state, nonce, code_challenge_method: "S256", code_challenge: challenge,
    };
    if (!registration) params.agent_name_hint = "noe";
    // Do not put retained ID tokens in a URL printed to the terminal or events.
    for (const [key, value] of Object.entries(params)) authorize.searchParams.set(key, value);
    writeJSON(pendingFile, { redirectURI });
    console.log("Continue with ChatGPT:\n" + authorize.href);
    console.log("On a remote host, run this helper with 'complete' in another terminal and paste the callback URL on stdin.");
    const callback = await callbackPromise;
    if (callback.searchParams.has("error")) throw new Error("ChatGPT sign-in was not authorized");
    const issuedId = callback.searchParams.get("client_id") || (registration && clientId);
    const code = callback.searchParams.get("code");
    if (!issuedId || issuedId === "dynamic_agent_client" || !code || (registration && issuedId !== clientId)) {
      throw new Error("ChatGPT callback is incomplete or changed the registered client");
    }
    // Keep issued registration even when a short-lived authorization code expires.
    writeJSON(registrationFile, { issuer, client_id: issuedId, ext_agent_host_id: hostId });
    const response = await fetch(discovery.token_endpoint, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ grant_type: "authorization_code", client_id: issuedId, code,
        code_verifier: verifier, redirect_uri: redirectURI, resource }).toString(),
    });
    if (!response.ok) throw new Error("ChatGPT code exchange failed (HTTP " + response.status + "); run login again");
    const tokens = await response.json();
    const account = await save_chatgpt_credentials({ file, clientId: issuedId, hostId, nonce, tokens });
    console.log("ChatGPT credentials saved: " + account.file);
  } finally {
    remove(pendingFile);
    // Close outside the HTTP handler, after its callback response has flushed.
    await new Promise(resolve => setTimeout(resolve, 0));
    server.close();
  }
} else {
  throw new Error("Unknown auth command. Use login, complete, or help.");
}
