// Run: node examples/agent-console/tests/ui-helpers.mjs
import assert from 'node:assert/strict';
import { actionRequest, availableActions, mergeRecords, windowSize } from '../public/timeline.js';

const page = (first, count) => Array.from({ length: count }, (_, index) => ({ seq: first + index }));
assert.equal(windowSize(5000), 1000);
assert.equal(windowSize(undefined), 1000);
assert.equal(windowSize(12), 12);
const latest = mergeRecords(page(1, 1000), page(1001, 200));
assert.equal(latest.length, 1000);
assert.equal(latest[0].seq, 201);
assert.equal(latest.at(-1).seq, 1200);
const older = mergeRecords(latest, page(1, 200), { direction: 'older', anchorSeq: 201 });
assert.equal(older.length, 1000);
assert.equal(older[0].seq, 1);
assert.equal(older.at(-1).seq, 1000);
const overlapped = mergeRecords(page(10, 20), page(20, 20), { limit: 25, anchorSeq: 15 });
assert.equal(overlapped.length, 25);
assert.equal(new Set(overlapped.map(record => record.seq)).size, 25);
assert.ok(overlapped.some(record => record.seq === 15));
assert.equal(mergeRecords(page(1, 10), [], { limit: 3 }).length, 3);

const idle = { state: 'idle', currentRunId: null, currentCommandId: null };
assert.deepEqual(availableActions(idle), { queue: true, steer: false, interrupt: false, stop: true });
const running = { state: 'running', currentRunId: 'run-1', currentCommandId: 'command-1' };
assert.deepEqual(availableActions(running), { queue: true, steer: true, interrupt: true, stop: true });
for (const disabled of [availableActions(running, true), availableActions({ state: 'stopped' }), availableActions({ state: 'failed' }), availableActions(running, false, true)]) {
  assert.ok(Object.values(disabled).every(value => value === false));
}
const steer = actionRequest('steer', { sessionId: 's', snapshot: running, instruction: '  original\ntext  ', commandId: 'steer-1' });
const interrupt = actionRequest('interrupt', { sessionId: 's', snapshot: running });
running.currentRunId = 'run-2';
running.currentCommandId = 'command-2';
assert.deepEqual(steer, { sessionId: 's', instruction: '  original\ntext  ', commandId: 'steer-1', runId: 'run-1' });
assert.deepEqual(interrupt, { sessionId: 's', commandId: 'command-1' });
assert.ok(Object.isFrozen(steer));
console.log('UI helpers: bounded paging, deduplication, anchor retention, availability, and frozen targets passed.');
