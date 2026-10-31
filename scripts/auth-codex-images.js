// Create a dedicated NOE-managed Codex image grant; active Codex credentials
// are never read or copied by this helper.
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { begin_codex_image_login, complete_codex_image_login } from "noe:auth";

const args = process.argv.slice(2);
const command = args[0] || "login";
const configDir = join(process.env.XDG_CONFIG_HOME || join(homedir(), ".config"), "noe");
const file = resolve(args[1] || process.env.NOE_CODEX_IMAGE_CREDENTIALS_FILE || join(configDir, "credentials", "openai-codex-images.json"));

if (command === "help" || command === "--help") {
  console.log("Usage: noe run auth-codex-images.js [login|help] [credential-file]");
  console.log("login: print a ChatGPT authorization URL and wait for the loopback callback.");
  console.log("Use a dedicated NOE credential file; this helper never reads the active Codex login.");
} else if (command === "login") {
  const attempt = await begin_codex_image_login({ file });
  console.log("Continue with ChatGPT:");
  console.log(attempt.authorizationURL);
  console.log("Waiting for callback at " + attempt.redirectURI);
  console.log("If NOE runs remotely, forward this port to the NOE host with SSH local port forwarding.");
  try {
    const account = await complete_codex_image_login(attempt.id);
    console.log("Codex image credentials saved: " + account.file);
  } catch (error) {
    throw new Error("Codex image sign-in failed: " + error.message);
  }
} else {
  throw new Error("Unknown auth command. Use login or help.");
}
