import { list_models, run_agent, spawn_agent, wait_task } from "noe:agent";
import { analyze_log } from "noe:agent-log";
import { save_chatgpt_credentials } from "noe:auth";
import { decide } from "noe:decision";
import { generate_image, read_image } from "noe:image";
import { generate } from "noe:model";

async function publicAPIExamples(): Promise<void> {
  const models = await list_models({kind: "agent"});
  const model: NOEAgent.ModelInfo | undefined = models[0];

  const snapshot = await run_agent({
    provider: "openrouter",
    model: "provider/model",
    effort: "auto",
    contextBudget: {inputTokens: 16_000},
    contextPolicy: {retainRatio: 0.2},
  }, "Summarize the project.");
  const answer: string | undefined = snapshot.lastResult?.answer;
  if (snapshot.context) {
    const budget: {inputTokens: number | null} = snapshot.context.budget;
    const modelWindow: {tokens: number | null; source: string | null} = snapshot.context.modelWindow;
    void [budget, modelWindow];
  }

  const idle = await spawn_agent(undefined, undefined, {keepAlive: true});
  const outcome = await wait_task({agentId: idle.agentId, commandId: "command-id", includeAnswer: false});
  const state: NOEAgent.TaskState = outcome.state;

  const decision = await decide({
    provider: "openrouter",
    model: "provider/decision-model",
    state: {text: "Choose a route"},
    questions: {route: {type: "choice", criteria: {fast: "Fast", safe: "Safe"}}},
  });
  const choice: NOEAgent.JSONValue | undefined = decision.answers.route;

  const image = await read_image("artifacts/input.png", {detail: "high"});
  const generated = await generate_image({
    provider: "openai-api",
    model: "provider/image-model",
    prompt: "A blue circle",
    output: "artifacts/circle.png",
    parameters: {quality: "low"},
  });

  const response = await generate({
    provider: "openrouter",
    model: "provider/model",
    messages: [{role: "user", content: "Hello"}],
  });
  const finishReason: string = response.finishReason;

  const saved = await save_chatgpt_credentials({
    file: "credentials.json",
    clientId: "client",
    hostId: "host",
    nonce: "nonce",
    tokens: {
      access_token: "access",
      id_token: "id",
      token_type: "Bearer",
      expires_in: 3600,
      scope: "chatgpt.tokens.use.direct",
    },
  });

  const log = analyze_log("", {runId: "run-id"});
  const logStatus: string = log.status;
  void [model, answer, state, choice, image, generated, finishReason, saved, logStatus];
}

void publicAPIExamples;
