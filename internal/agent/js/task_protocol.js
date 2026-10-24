import { request } from "noe:agent/internal";

// Private JSON request boundary shared by the task loop and tool adapters.
export async function rpc(method, payload) {
  return JSON.parse(await request(method, JSON.stringify(payload)));
}

export function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
