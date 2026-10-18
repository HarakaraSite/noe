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

  add(kind, messages) {
    this.entries.push({id: "history-" + (++this.sequence), kind, messages});
    this.revision++;
  }

  appendSteer(command) {
    if (this.steers.has(command.commandId)) return;
    this.add("protected", [{role: "user", content: command.instruction}]);
    this.steers.add(command.commandId);
  }

  completeBlock(assistant, results = []) {
    const calls = assistant.tool_calls || [];
    if (calls.length !== results.length) throw Error("incomplete history block");
    const messages = [assistant];
    for (let i = 0; i < calls.length; i++) {
      messages.push({role: "tool", tool_call_id: calls[i].id, content: JSON.stringify(results[i])});
    }
    this.add("block", messages);
  }

  messages() { return this.entries.flatMap(entry => entry.messages); }

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
