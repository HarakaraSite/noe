import { save, beginCodexImageLogin, completeCodexImageLogin } from "noe:auth/internal";

// The external helper handles the browser; Go verifies the ID token and saves.
export async function save_chatgpt_credentials(options) {
  if (options === null || typeof options !== "object" || Array.isArray(options)) {
    throw new TypeError("sign-in result must be an object");
  }
  return JSON.parse(await save(JSON.stringify(options)));
}

// Managed Codex image login keeps callback handling and token exchange native.
export async function begin_codex_image_login(options) {
  if (options === null || typeof options !== "object" || Array.isArray(options) || typeof options.file !== "string" || !options.file) {
    throw new TypeError("Codex image login requires a credential file");
  }
  return JSON.parse(await beginCodexImageLogin(JSON.stringify({file: options.file})));
}

export async function complete_codex_image_login(id) {
  if (typeof id !== "string" || !id) throw new TypeError("Codex image login attempt ID is required");
  return JSON.parse(await completeCodexImageLogin(id));
}
