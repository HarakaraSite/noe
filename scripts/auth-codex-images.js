// Create a dedicated NOE-managed Codex image grant; active Codex credentials
// are never read or copied by this helper.
import { homedir } from "node:os";
import { stdin } from "noe:process";
import { join, resolve } from "node:path";
import { readFileSync, writeFileSync, existsSync, readdirSync, statSync, mkdirSync, chmodSync, renameSync, unlinkSync } from "noe:fs";
import { begin_codex_image_login, complete_codex_image_login } from "noe:auth";

const args = process.argv.slice(2);
const command = args[0] || "login";
const configDir = join(process.env.XDG_CONFIG_HOME || join(homedir(), ".config"), "noe");
const file = resolve(args[1] || process.env.NOE_CODEX_IMAGE_CREDENTIALS_FILE || join(configDir, "credentials", "openai-codex-images.json"));
const pendingDir = file + ".pending";

function pendingPath(id) { return join(pendingDir, id + ".json"); }

function readPending() {
  try {
    let latest = "", latestTime = -Infinity;
    for (const name of readdirSync(pendingDir)) {
      if (!name.endsWith(".json")) continue;
      const path = join(pendingDir, name);
      let modified;
      try { modified = statSync(path).mtimeMs; } catch { continue; }
      if (modified > latestTime || (modified === latestTime && path > latest)) {
        latest = path;
        latestTime = modified;
      }
    }
    if (!latest) throw new Error();
    return JSON.parse(readFileSync(latest, "utf8"));
  } catch {
    throw new Error("No readable pending image sign-in. Start login in terminal A, then run complete with the same credential-file in terminal B.");
  }
}

function writePending(attempt) {
  const path = pendingPath(attempt.id);
  const temp = path + ".tmp";
  try {
    mkdirSync(pendingDir, { recursive: true });
    chmodSync(pendingDir, 0o700);
    writeFileSync(temp, JSON.stringify({ redirectURI: attempt.redirectURI }));
    chmodSync(temp, 0o600);
    renameSync(temp, path);
  } catch {
    throw new Error("Could not record the pending image sign-in. Check the credential directory permissions and run login again.");
  } finally {
    if (existsSync(temp)) unlinkSync(temp);
  }
}

function removePending(id) {
  // Unique per-attempt paths let overlapping logins clean up independently.
  const path = pendingPath(id);
  if (existsSync(path)) unlinkSync(path);
}

if (command === "help" || command === "--help") {
  console.log("Usage: noe run --no-inst scripts/auth-codex-images.js [login|complete|help] [credential-file]");
  console.log("login: print a ChatGPT authorization URL and wait for the loopback callback.");
  console.log("complete: paste the browser's callback URL on stdin, then send EOF (Enter, Ctrl+D).");
} else if (command === "complete") {
  console.log("Paste the full callback URL from the browser, press Enter, then press Ctrl+D on the empty line:");
  const pending = readPending();
  let expected;
  try {
    expected = new URL(pending.redirectURI);
    if (expected.protocol !== "http:" || expected.hostname !== "127.0.0.1" || expected.pathname !== "/auth/callback" || expected.search || expected.hash || expected.username || expected.password) throw new Error();
  } catch {
    throw new Error("Invalid pending image callback receiver. Run login again with the same credential-file.");
  }
  let callback;
  try {
    callback = new URL((await new Response(stdin).text()).trim());
  } catch {
    throw new Error("Invalid callback URL. Copy the full redirected URL from the browser and run complete again.");
  }
  if (callback.origin !== expected.origin || callback.pathname !== expected.pathname || callback.hash || callback.username || callback.password) {
    throw new Error("Callback URL does not match the running image sign-in. Use its browser redirect URL and the same credential-file, or run login again.");
  }
  let response;
  try {
    response = await fetch(callback.href);
  } catch {
    // Native fetch errors may include the URL and its authorization code.
    throw new Error("The image sign-in listener is no longer reachable. Run login again in terminal A, then retry complete in terminal B with the new browser redirect URL.");
  }
  if (!response.ok) {
    throw new Error("The running image sign-in rejected the callback. Check terminal A; if login has ended, start login again and copy its new browser redirect URL.");
  }
  console.log("Callback delivered. Check the login terminal for the result.");
} else if (command === "login") {
  const attempt = await begin_codex_image_login({ file });
  try {
    writePending(attempt);
    console.log("Continue with ChatGPT:\n" + attempt.authorizationURL);
    console.log("On a remote host, keep this login running and run this helper with 'complete' in another terminal.");
    console.log("Copy the full http://127.0.0.1:.../auth/callback?... URL from the browser address bar, even if the page cannot connect.");
    console.log("Paste that URL into the complete terminal, press Enter, then press Ctrl+D on the empty line.");
    console.log("Check this login terminal for confirmation that credentials were saved.");
    const account = await complete_codex_image_login(attempt.id);
    console.log("Codex image credentials saved: " + account.file);
  } finally {
    removePending(attempt.id);
  }
} else {
  throw new Error("Unknown auth command. Use login, complete, or help.");
}
