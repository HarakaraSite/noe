import { rpc } from "./host.js";

const tools = [{
  type: "function",
  function: {
    name: "run_js",
    description: "Execute JavaScript as an ES module in a fresh NOE child process. Returns stdout, stderr, exitCode and reason. Variables do not persist; files do. The bindings createAgent and webSearch are already available. Use console.log to return data. Supports await, fs/promises, fetch and child_process.",
    parameters: { type: "object", properties: { code: { type: "string" } }, required: ["code"], additionalProperties: false },
  },
}];

const system = `You carry out the user's task autonomously. Use run_js whenever computation, files, services or delegation are needed. Read actual inputs before reporting facts. Inspect tool results and repair recoverable failures. Do not request human input or approval; return a final answer or explain why the task cannot be completed.
run_js starts a new NOE JavaScript process per call. Standard modules include fs/promises and child_process. All processes use the configured working directory. Fixture files are at process.env.NOE_AGENT_FIXTURES. Use console.log(JSON.stringify(value)) to return structured data.
For delegation, run_js code can use the prebound createAgent({name,system}) function. Its async run(instruction,{cacheKey,maxSteps}) returns the final answer string. Use Promise.all for independent children. A child can itself use run_js. Each agent has a separate history. For the spike, the expected delegation pattern is parent -> child agents -> JS execution.
The prebound webSearch(query) function calls a Go-hosted AUTHENTICATED LOCAL FIXTURE, not the Internet. It returns {source,query,results}. Do not describe its contents as real web search results. Authentication is handled by the host; no credentials are required in your code.`;

export function createAgent({ name = "agent", system: extra = "" } = {}) {
  return {
    async run(instruction, { cacheKey = "", maxSteps = Number(process.env.NOE_AGENT_MAX_STEPS || 0) } = {}) {
      if (typeof instruction !== "string" || !instruction.trim()) throw new TypeError("instruction must be a nonempty string");
      if (!Number.isInteger(maxSteps) || maxSteps < 0) throw new TypeError("maxSteps must be a nonnegative integer");
      const { runId } = await rpc("/run/start", { name });
      let status = "failed";
      try {
        const messages = [{ role: "system", content: system + (extra ? "\n" + extra : "") }, { role: "user", content: instruction }];
        for (let step = 0; !maxSteps || step < maxSteps; step++) {
          const response = await rpc("/model", { runId, messages, tools, cacheKey });
          const message = response.message;
          if (!message || message.role !== "assistant") throw new Error("invalid assistant response");
          if (response.finishReason !== "stop" && response.finishReason !== "tool_calls") {
            throw new Error(`model did not complete its turn: ${response.finishReason}`);
          }
          // Preserve the entire message, including provider reasoning blocks.
          messages.push(message);
          const calls = message.tool_calls || [];
          if (!calls.length) {
            if (response.finishReason !== "stop" || typeof message.content !== "string" || !message.content.trim()) throw new Error("model returned no final answer");
            status = "completed";
            return message.content;
          }
          for (const call of calls) {
            let result;
            try {
              if (call.type !== "function" || call.function?.name !== "run_js") throw new Error("unknown tool");
              const args = JSON.parse(call.function.arguments);
              if (typeof args.code !== "string") throw new Error("run_js requires a code string");
              result = await rpc("/js", { runId, code: args.code });
            } catch (error) {
              // Invalid arguments are repairable by the model. Host/transport
              // failures terminate the run instead of entering a retry loop.
              if (error.code) throw error;
              result = { stdout: "", stderr: error.message, exitCode: 1, reason: "tool_error" };
            }
            messages.push({ role: "tool", tool_call_id: call.id, content: JSON.stringify(result) });
          }
        }
        throw new Error(`agent exceeded maxSteps (${maxSteps})`);
      } finally {
        // Keep the original failure if the host is already shutting down.
        await rpc("/run/finish", { runId, status }).catch(() => {});
      }
    },
  };
}
