// Pure helpers shared by the browser controller and focused UI tests.
export const MAX_WINDOW_SIZE = 1000;

export function windowSize(value) {
  return Number.isFinite(value) && value > 0
    ? Math.min(MAX_WINDOW_SIZE, Math.max(1, Math.floor(value)))
    : MAX_WINDOW_SIZE;
}

// A page replaces a discarded edge, not an unbounded client-side archive.
// Keep the first visible record when trimming, so its pixel offset can be restored.
export function mergeRecords(current, incoming, { limit = MAX_WINDOW_SIZE, direction = 'newer', anchorSeq } = {}) {
  const cap = windowSize(limit);
  const bySeq = new Map(current.map(record => [record.seq, record]));
  for (const record of incoming) bySeq.set(record.seq, record);
  const sorted = [...bySeq.values()].sort((a, b) => a.seq - b.seq);
  let start = direction === 'older' ? 0 : Math.max(0, sorted.length - cap);
  const anchorIndex = sorted.findIndex(record => record.seq === anchorSeq);
  if (anchorIndex >= 0) {
    if (anchorIndex < start) start = anchorIndex;
    if (anchorIndex >= start + cap) start = anchorIndex - cap + 1;
  }
  return sorted.slice(start, start + cap);
}

export function availableActions(snapshot, readOnly = false, pending = false) {
  const live = Boolean(snapshot) && !readOnly && !pending && !['stopped', 'stopping', 'failed'].includes(snapshot.state);
  const running = live && snapshot.state !== 'idle' && Boolean(snapshot.currentRunId);
  return {
    queue: live,
    steer: running,
    interrupt: running && Boolean(snapshot.currentCommandId),
    stop: live,
  };
}

// Construct once, at click time. No later snapshot may change these target IDs.
export function actionRequest(operation, { sessionId, snapshot, instruction, commandId }) {
  const body = { sessionId };
  if (operation === 'queue' || operation === 'steer') {
    body.instruction = instruction;
    if (commandId) body.commandId = commandId;
  }
  if (operation === 'steer') {
    if (!snapshot.currentRunId) throw new Error('実行中の run がありません。');
    body.runId = snapshot.currentRunId;
  }
  if (operation === 'interrupt') {
    if (!snapshot.currentCommandId) throw new Error('実行中の command がありません。');
    body.commandId = snapshot.currentCommandId;
  }
  return Object.freeze(body);
}
