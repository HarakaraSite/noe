import { rpc } from "./host.js";

export async function webSearch(query) {
  if (typeof query !== "string") throw new TypeError("query must be a string");
  return rpc("/service/search", { runId: process.env.NOE_AGENT_OWNER_RUN, query });
}
