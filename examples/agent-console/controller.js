import { randomUUID } from "node:crypto";

export class HTTPError extends Error {
  constructor(status, message, record = undefined) {
    super(message);
    this.status = status;
    this.record = record;
  }
}

function requiredText(value, name) {
  if (typeof value !== "string" || !value.trim()) throw new HTTPError(400, name + " is required");
  return value;
}

export class AgentController {
  constructor(api, store, config, agentId, pollInterval = 1000) {
    this.api = api;
    this.store = store;
    this.config = config;
    this.agentId = agentId;
    this.pollInterval = pollInterval;
    this.snapshot = null;
    this.error = null;
    this.previous = "";
    this.timer = undefined;
    this.closed = false;
    this.operations = Promise.resolve();
    this.results = Promise.resolve();
    this.pending = new Map();
  }

  fail(error) {
    const message = error instanceof Error ? error.message : String(error);
    if (this.error !== message) console.error("Agent console:", message);
    this.error = message;
  }

  async refresh() {
    const snapshot = await this.api.observe_agent({ agentId: this.agentId, includeAnswer: false });
    this.snapshot = snapshot;
    const value = JSON.stringify(snapshot);
    if (value !== this.previous) {
      await this.store.append({ type: "observe", snapshot });
      this.previous = value;
    }
  }

  async start() {
    await this.refresh();
    const tick = async () => {
      try {
        await this.refresh();
      } catch (error) {
        this.fail(error);
      }
      if (!this.closed) this.timer = setTimeout(tick, this.pollInterval);
    };
    this.timer = setTimeout(tick, this.pollInterval);
  }

  close() {
    this.closed = true;
    clearTimeout(this.timer);
  }

  state() {
    return {
      sessionId: this.store.sessionId,
      model: { provider: this.config.provider, model: this.config.model, effort: this.config.effort },
      snapshot: this.snapshot,
      error: this.error,
      windowSize: 1000,
      pollInterval: this.pollInterval,
    };
  }

  operation(operation, body) {
    const next = this.operations.then(() => this.perform(operation, body));
    this.operations = next.then(() => {}, () => {});
    return next;
  }

  async perform(operation, body) {
    if (body.sessionId !== this.store.sessionId) throw new HTTPError(409, "This session is read-only or no longer current");
    if (this.error && operation !== "stop") throw new HTTPError(503, this.error);
    const target = { agentId: this.agentId };
    const payload = { type: "operation", operation, target };
    let request;
    if (operation === "queue" || operation === "steer") {
      payload.instruction = requiredText(body.instruction, "instruction");
      payload.commandId = body.commandId === undefined ? randomUUID() : requiredText(body.commandId, "commandId");
      if (operation === "queue") {
        request = { agentId: this.agentId, instruction: payload.instruction, commandId: payload.commandId };
      } else {
        target.runId = requiredText(body.runId, "runId");
        request = { runId: target.runId, instruction: payload.instruction, commandId: payload.commandId };
      }
    } else if (operation === "interrupt") {
      target.commandId = requiredText(body.commandId, "commandId");
      request = { agentId: this.agentId, commandId: target.commandId };
    } else if (operation === "stop") {
      request = { agentId: this.agentId };
    } else {
      throw new HTTPError(404, "Unknown operation");
    }
    const method = { queue: "queue_agent", steer: "steer_agent", interrupt: "cancel_task", stop: "stop_agent" }[operation];
    let receipt;
    let apiError;
    try {
      receipt = await this.api[method](request);
      payload.receipt = receipt;
    } catch (error) {
      apiError = error instanceof Error ? error.message : String(error);
      payload.error = apiError;
    }
    let record;
    try {
      record = await this.store.append(payload);
    } catch (error) {
      this.fail(error);
      throw error;
    }
    if (apiError) throw new HTTPError(409, apiError, record);
    if (operation === "queue") this.track(receipt.commandId);
    return { receipt, record };
  }

  track(commandId) {
    if (this.pending.has(commandId) || this.store.hasResult(this.store.sessionId, commandId)) return;
    const collecting = this.collect(commandId).catch(error => this.fail(error));
    this.pending.set(commandId, collecting);
    collecting.then(() => this.pending.delete(commandId));
  }

  async collect(commandId) {
    const outcome = await this.api.wait_task({ agentId: this.agentId, commandId, includeAnswer: false });
    const next = this.results.then(async () => {
      let offset = 0;
      const parts = [];
      while (true) {
        const part = await this.api.read_task_result({ agentId: this.agentId, commandId, offset });
        parts.push(part.text);
        if (part.eof) break;
        offset = part.nextOffset;
      }
      await this.store.append({ ...outcome, type: "result", answer: parts.join("") });
    });
    this.results = next.then(() => {}, () => {});
    await next;
  }
}
