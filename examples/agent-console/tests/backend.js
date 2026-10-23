import { appendFile, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { AgentController } from "../controller.js";
import { SessionStore } from "../store.js";

function assert(condition, message) {
  if (!condition) throw new Error(message);
}
const config = { provider: "fixture", model: "fixture/model", effort: "auto", name: "worker" };
const details = { agentId: "fixture-agent", name: "worker", model: config };
const root = await mkdtemp(resolve("noe-console-test-"));
try {
  const store = await SessionStore.create(root, details);
  const queued = await store.append({ type: "operation", operation: "queue", commandId: "done", receipt: { commandId: "done", status: "accepted" } });
  await store.append({ type: "operation", operation: "queue", commandId: "unfinished", receipt: { commandId: "unfinished", status: "accepted" } });
  const answer = "日本語🌱\n".repeat(1000) + "ANSWER END\n";
  await store.append({ type: "result", commandId: "done", runId: "run-done", state: "completed", answer });
  await Promise.all(Array.from({ length: 1105 }, (_, index) => store.append({ type: "observe", snapshot: { state: "running", index } })));
  const latest = await store.history({ limit: 1000 });
  assert(latest.records.length === 1000 && latest.firstSeq === 109 && latest.lastSeq === 1108, "latest window bounds");
  assert(latest.records.every((record, index) => record.seq === latest.firstSeq + index), "append sequencing");
  const earlier = await store.history({ before: latest.firstSeq, limit: 1000 });
  assert(earlier.records.length === 108 && earlier.lastSeq === 108, "older page is exclusive");
  assert(earlier.records[2].answer === answer, "original multiline Unicode answer preserved");
  const forward = await store.history({ after: 108, limit: 100 });
  assert(forward.firstSeq === 109 && forward.lastSeq === 208, "newer page is exclusive");
  const empty = await store.history({ after: latest.total, limit: 100 });
  assert(empty.records.length === 0 && empty.total === 1108, "caught-up cursor");
  for (const query of [{ limit: 1001 }, { limit: 0 }, { after: -1 }, { before: 0 }, { before: 2, after: 0 }, { sessionId: "../outside" }]) {
    let failed = false;
    try { await store.history(query); } catch { failed = true; }
    assert(failed, "invalid history query rejected: " + JSON.stringify(query));
  }
  const historyPath = join(root, store.sessionId, "history.jsonl");
  const saved = await readFile(historyPath, "utf8");
  assert(saved.split("\n").length === 1109, "view paging never deletes saved records");
  // Rebuild disposable indexes after a restart, ignoring a torn final append.
  await writeFile(join(root, store.sessionId, "offsets.idx"), "bad disposable index");
  await appendFile(historyPath, '{"seq":1109,"type":"result"');
  const restarted = await SessionStore.create(root, details);
  const past = await restarted.history({ sessionId: store.sessionId, before: 4, limit: 100 });
  assert(past.total === 1108 && past.records.length === 3, "complete JSONL prefix recovered");
  assert(past.records[0].resultRecorded === true && past.records[1].resultRecorded === false, "historical unresolved tasks distinguished");
  assert(past.records[2].answer === answer && queued.seq === 1, "old results remain intact");
  assert((await readFile(historyPath, "utf8")) === saved + '{"seq":1109,"type":"result"', "past history remains read-only");
  const sessions = await restarted.sessions();
  assert(sessions.sessions.length === 2 && sessions.currentSessionId === restarted.sessionId, "new session without old agent/queue restoration");
  const incompleteId = "11111111-1111-4111-8111-111111111111";
  const malformedId = "22222222-2222-4222-8222-222222222222";
  await mkdir(join(root, incompleteId, "results"), { recursive: true });
  await mkdir(join(root, malformedId), { recursive: true });
  await writeFile(join(root, malformedId, "metadata.json"), '{"sessionId":');
  // Staging from an interrupted new-format initialization is never published.
  await mkdir(join(root, ".initializing-33333333-3333-4333-8333-333333333333"));
  const recovered = await restarted.sessions();
  assert(recovered.sessions.length === 4 && recovered.sessions.filter(session => session.error).length === 2,
    "incomplete legacy metadata is reported without hiding healthy sessions");
  assert(recovered.sessions.some(session => session.sessionId === store.sessionId && !session.error) &&
    recovered.sessions.some(session => session.current && session.sessionId === restarted.sessionId), "healthy past and current sessions remain listed");
  assert((await restarted.history({ sessionId: store.sessionId, before: 4 })).records[2].answer === answer,
    "healthy history remains readable after an interrupted initialization");
  assert((await readdir(root)).filter(name => name.startsWith(".initializing-")).length === 1,
    "successful session initialization publishes its staging directory");

  const controls = await SessionStore.create(root, details);
  let snapshot = { agentId: "fixture-agent", state: "idle", currentRunId: "", queueLength: 0, lastResult: null };
  const collectedOptions = [];
  const reads = [];
  let resolveTask = () => {};
  const taskDone = new Promise(resolve => { resolveTask = () => resolve(undefined); });
  const api = {
    observe_agent: async options => { collectedOptions.push(options); return snapshot; },
    queue_agent: async options => ({ commandId: options.commandId, status: "accepted" }),
    wait_task: async options => { collectedOptions.push(options); await taskDone; return { commandId: options.commandId, runId: "run-1", state: "completed", answerBytes: new TextEncoder().encode(answer).length }; },
    read_task_result: async options => {
      reads.push(options.offset);
      const parts = [answer.slice(0, 4000), answer.slice(4000, 8000), answer.slice(8000)];
      const index = options.offset === 0 ? 0 : options.offset === 1 ? 1 : 2;
      return { text: parts[index], nextOffset: index + 1, eof: index === 2 };
    },
    steer_agent: async options => ({ commandId: options.commandId, status: "accepted" }),
    cancel_task: async options => ({ ...options, status: "accepted" }),
    stop_agent: async options => ({ ...options, status: "accepted" }),
  };
  const controller = new AgentController(api, controls, config, "fixture-agent");
  await controller.start();
  controller.close();
  await controller.refresh();
  assert((await controls.history({ limit: 100 })).total === 1, "identical snapshots not recorded twice");
  snapshot = { ...snapshot, state: "running", currentRunId: "run-1" };
  await controller.refresh();
  const accepted = await controller.operation("queue", { sessionId: controls.sessionId, instruction: "  original\ntext  ", commandId: "task-1" });
  resolveTask();
  await Promise.all([...controller.pending.values()]);
  const records = (await controls.history({ limit: 100 })).records;
  assert(records[1].snapshot.state === "running" && records[2].instruction === "  original\ntext  ", "snapshots and instruction text not summarized");
  assert(records[3].answer === answer && reads.join(",") === "0,1,2", "read_task_result follows every nextOffset to eof");
  assert(collectedOptions.every(options => options.includeAnswer === false), "snapshot/wait calls omit repeated answers");
  await controller.operation("queue", { sessionId: controls.sessionId, instruction: "  original\ntext  ", commandId: accepted.receipt.commandId });
  assert((await controls.history({ limit: 100 })).records.filter(record => record.type === "result").length === 1, "queue retry does not duplicate retained result");
  const stale = await controller.operation("steer", { sessionId: controls.sessionId, instruction: "guidance", runId: "old-run", commandId: "steer-1" });
  assert(stale.record.target.runId === "old-run", "steer target never replaced by a newer run");
  console.log("Agent console backend tests passed: paging, restart, full results, snapshots, fixed targets.");
} finally {
  await rm(root, { recursive: true, force: true });
}
