// Private transcript storage. Entries are classified at their append points;
// provider objects are kept intact, including reasoning and native items.
export class TaskHistory {
  constructor(system, task) {
    this.entries = [];
    this.revision = 0;
    this.sequence = 0;
    this.steers = new Set();
    this.add("protected", [system]);
    this.add("protected", [task]);
  }

  static restore(snapshot) {
    if (snapshot === null || typeof snapshot !== "object" || Array.isArray(snapshot) ||
        !Number.isSafeInteger(snapshot.revision) || snapshot.revision < 2 ||
        !Number.isSafeInteger(snapshot.sequence) || snapshot.sequence < 2 ||
        snapshot.sequence > snapshot.revision || !Array.isArray(snapshot.entries) || snapshot.entries.length < 2) {
      throw new Error("agent broker returned an invalid conversation snapshot");
    }
    const history = Object.create(TaskHistory.prototype);
    history.revision = snapshot.revision;
    history.sequence = snapshot.sequence;
    history.steers = new Set();
    const seen = new Set();
    history.entries = snapshot.entries.map((entry) => {
      if (entry === null || typeof entry !== "object" || Array.isArray(entry) ||
          typeof entry.id !== "string" || !entry.id || seen.has(entry.id) ||
          (entry.kind !== "protected" && entry.kind !== "block") || !Array.isArray(entry.messages) || entry.messages.length === 0) {
        throw new Error("agent broker returned an invalid conversation entry");
      }
      seen.add(entry.id);
      if (entry.messages.some(message => message === null || typeof message !== "object" || Array.isArray(message))) {
        throw new Error("agent broker returned an invalid conversation message");
      }
      if (entry.kind === "protected" && entry.messages.length !== 1) {
        throw new Error("agent broker returned an invalid protected conversation entry");
      }
      if (entry.kind === "block") validateCompleteBlock(entry.messages);
      return {id: entry.id, kind: entry.kind, messages: entry.messages};
    });
    return history;
  }

  add(kind, messages) {
    this.entries.push({id: "history-" + (++this.sequence), kind, messages});
    this.revision++;
  }

  appendSteer(command) {
    if (this.steers.has(command.commandId)) return;
    this.add("protected", [{role: "user", content: command.instruction}]);
    this.steers.add(command.commandId);
  }

  completeBlock(assistant, results = [], images = undefined, contextMessage = undefined) {
    const calls = assistant.tool_calls || [];
    if (calls.length !== results.length) throw Error("incomplete history block");
    const messages = contextMessage === undefined ? [assistant] : [contextMessage, assistant];
    for (let i = 0; i < calls.length; i++) {
      messages.push({role: "tool", tool_call_id: calls[i].id, content: JSON.stringify(results[i])});
    }
    if (images !== undefined && images !== null) {
      if (!Array.isArray(images)) throw Error("invalid image history references");
      const seenIDs = new Set();
      const parts = images.map(image => {
        if (image === null || typeof image !== "object" || Array.isArray(image) ||
            typeof image.imageId !== "string" || !image.imageId || seenIDs.has(image.imageId)) {
          throw Error("invalid image history references");
        }
        seenIDs.add(image.imageId);
        return {type: "noe_image", imageId: image.imageId};
      });
      if (parts.length > 0) messages.push({role: "user", content: parts});
    }
    this.add("block", messages);
  }

  messages() { return this.entries.flatMap(entry => entry.messages); }

  snapshot() {
    return {
      revision: this.revision,
      sequence: this.sequence,
      entries: this.entries.map(entry => ({id: entry.id, kind: entry.kind, messages: entry.messages})),
    };
  }

  layout() {
    return {
      revision: this.revision,
      entries: this.entries.map(entry => ({id: entry.id, kind: entry.kind, count: entry.messages.length})),
    };
  }

  stage(plan) {
    if (!plan || plan.revision !== this.revision || !Array.isArray(plan.removeIds) || !plan.removeIds.length) {
      throw Error("stale or invalid context trim plan");
    }
    const blocks = this.entries.filter(entry => entry.kind === "block");
    // A deletion must be a strict prefix of complete blocks. The most recent
    // block and every protected entry survive, in their original order.
    if (plan.removeIds.length >= blocks.length || plan.removeIds.some((id, i) => id !== blocks[i].id)) {
      throw Error("context trim plan would remove protected or recent history");
    }
    const removed = new Set(plan.removeIds);
    const entries = this.entries.filter(entry => !removed.has(entry.id));
    return {fromRevision: this.revision, entries,
      messages: entries.flatMap(entry => entry.messages),
      snapshot: {revision: this.revision + 1, sequence: this.sequence,
        entries: entries.map(entry => ({id: entry.id, kind: entry.kind, messages: entry.messages}))},
      layout: {revision: this.revision + 1,
        entries: entries.map(entry => ({id: entry.id, kind: entry.kind, count: entry.messages.length}))}};
  }

  commit(staged) {
    if (staged.fromRevision !== this.revision) throw Error("stale context trim commit");
    this.entries = staged.entries;
    this.revision = staged.layout.revision;
    return this.layout();
  }

  apply(plan) { return this.commit(this.stage(plan)); }
}

function validateCompleteBlock(messages) {
  if (messages[0]?.role === "user" && typeof messages[0].content === "string" &&
      messages[0].content.startsWith("NOE_CONTEXT_STATUS\n")) messages = messages.slice(1);
  const assistant = messages[0];
  if (!assistant || assistant.role !== "assistant") throw new Error("agent broker returned an incomplete conversation block");
  const calls = assistant.tool_calls === undefined || assistant.tool_calls === null ? [] : assistant.tool_calls;
  if (!Array.isArray(calls) || (messages.length !== calls.length + 1 && messages.length !== calls.length + 2)) {
    throw new Error("agent broker returned an incomplete conversation block");
  }
  const seen = new Set();
  for (let index = 0; index < calls.length; index++) {
    const call = calls[index];
    const result = messages[index + 1];
    if (call === null || typeof call !== "object" || typeof call.id !== "string" || !call.id || seen.has(call.id) ||
        result.role !== "tool" || result.tool_call_id !== call.id) {
      throw new Error("agent broker returned an incomplete conversation block");
    }
    seen.add(call.id);
  }
  if (messages.length === calls.length + 2) {
    const imageMessage = messages[messages.length - 1];
    if (imageMessage.role !== "user" || !Array.isArray(imageMessage.content) || imageMessage.content.length === 0) {
      throw new Error("agent broker returned invalid image references in a conversation block");
    }
    const imageIDs = new Set();
    for (const part of imageMessage.content) {
      if (part === null || typeof part !== "object" || Array.isArray(part) || part.type !== "noe_image" ||
          typeof part.imageId !== "string" || !part.imageId || imageIDs.has(part.imageId)) {
        throw new Error("agent broker returned invalid image references in a conversation block");
      }
      imageIDs.add(part.imageId);
    }
  }
}
