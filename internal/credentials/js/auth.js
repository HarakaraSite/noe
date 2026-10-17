import { save } from "noe:auth/internal";

// The external helper handles the browser; Go verifies the ID token and saves.
export async function save_chatgpt_credentials(options) {
  if (options === null || typeof options !== "object" || Array.isArray(options)) {
    throw new TypeError("sign-in result must be an object");
  }
  return JSON.parse(await save(JSON.stringify(options)));
}
