// Run with an existing Playwright installation (no runtime dependency for the console):
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node examples/agent-console/tests/ui-browser.mjs
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const model = { provider: 'fixture', model: 'ui-test', effort: 'low' };
const snapshot = { agentId: 'agent-live', name: 'worker', state: 'idle', currentRunId: null, currentCommandId: null, queueLength: 0, nested: { untouched: ['all', 'snapshot', 'fields'] } };
const liveState = { sessionId: 'live', model, snapshot, error: null, windowSize: 1000, pollInterval: 1000 };
const original = '  元の指示\n<img src=x onerror="globalThis.uiInjected=true"> & <script>bad()</script>  ';
const answer = ('長い回答 <img onerror="globalThis.uiInjected=true"> & text\n').repeat(200) + '回答の最後';
const stamp = '2026-01-02T03:04:05.000Z';
const histories = {
  live: [
    { seq: 1, time: stamp, type: 'observe', snapshot: structuredClone(snapshot) },
    { seq: 2, time: stamp, type: 'operation', operation: 'queue', instruction: original, target: { agentId: 'agent-live' }, commandId: 'q-1', receipt: { status: 'accepted' } },
    { seq: 3, time: stamp, type: 'result', commandId: 'q-1', runId: 'r-1', state: 'completed', answer },
  ],
  past: [
    { seq: 1, time: stamp, type: 'observe', snapshot: { ...snapshot, name: 'old-worker', state: 'running', currentRunId: 'old-run' } },
    { seq: 2, time: stamp, type: 'operation', operation: 'queue', instruction: original, target: { agentId: 'old-agent' }, commandId: 'unresolved', receipt: { status: 'accepted' }, resultRecorded: false },
    { seq: 3, time: stamp, type: 'operation', operation: 'queue', instruction: 'resolved', target: { agentId: 'old-agent' }, commandId: 'resolved', receipt: { status: 'accepted' }, resultRecorded: true },
    { seq: 4, time: stamp, type: 'result', commandId: 'resolved', runId: 'old-run', state: 'completed', answer },
  ],
};
const posts = [];
const historyRequests = [];
function append(record) {
  const result = { seq: histories.live.length + 1, time: stamp, ...record };
  histories.live.push(result);
  return result;
}
function accepted(operation, body) {
  const receipt = { status: 'accepted', commandId: body.commandId };
  const record = append({ type: 'operation', operation, instruction: body.instruction, target: { agentId: snapshot.agentId, runId: body.runId, commandId: operation === 'interrupt' ? body.commandId : undefined }, commandId: body.commandId, receipt });
  return { status: 200, data: { receipt, record } };
}
let postResponder = async (operation, body) => accepted(operation, body);
let stateUnavailable = false;
const csp = "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'";
const server = createServer(async (req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1');
  const json = (data, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(data)); };
  try {
    if (url.pathname === '/api/state') return stateUnavailable ? json({ error: 'fixture disconnected' }, 503) : json(liveState);
    if (url.pathname === '/api/sessions') return json({ currentSessionId: 'live', sessions: [
      { sessionId: 'live', startedAt: stamp, agentId: 'agent-live', name: 'worker', model, current: true },
      { sessionId: 'past', startedAt: stamp, agentId: 'old-agent', name: 'old-worker', model, current: false },
      { sessionId: 'broken', startedAt: '', name: '読み込み不可', error: 'metadata.json is missing', current: false },
    ] });
    if (url.pathname === '/api/history') {
      const sessionId = url.searchParams.get('session');
      const cap = Number(url.searchParams.get('limit'));
      assert.ok(cap > 0 && cap <= 1000);
      historyRequests.push({ sessionId, cap });
      const all = histories[sessionId];
      let records;
      if (url.searchParams.has('before')) records = all.filter(record => record.seq < Number(url.searchParams.get('before'))).slice(-cap);
      else if (url.searchParams.has('after')) records = all.filter(record => record.seq > Number(url.searchParams.get('after'))).slice(0, cap);
      else records = all.slice(-cap);
      return json({ sessionId, records, firstSeq: records[0]?.seq || 0, lastSeq: records.at(-1)?.seq || 0, total: all.length });
    }
    if (req.method === 'POST' && url.pathname.startsWith('/api/')) {
      let text = '';
      for await (const chunk of req) text += chunk;
      const body = JSON.parse(text);
      const operation = url.pathname.slice(5);
      posts.push({ operation, body, origin: req.headers.origin, contentType: req.headers['content-type'] });
      const result = await postResponder(operation, body);
      return json(result.data, result.status);
    }
    const files = { '/': ['index.html', 'text/html'], '/app.js': ['app.js', 'text/javascript'], '/timeline.js': ['timeline.js', 'text/javascript'], '/style.css': ['style.css', 'text/css'] };
    const file = files[url.pathname];
    if (!file) { res.writeHead(404); res.end(); return; }
    const contents = await readFile(fileURLToPath(new URL('../public/' + file[0], import.meta.url)));
    res.writeHead(200, { 'Content-Type': file[1], 'Content-Security-Policy': csp });
    res.end(contents);
  } catch (error) { json({ error: error.message }, 500); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = 'http://127.0.0.1:' + server.address().port;
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1100, height: 850 } });
const pageErrors = [];
page.on('pageerror', error => pageErrors.push(error.message));
await page.addInitScript(() => {
  globalThis.uiCspViolations = [];
  document.addEventListener('securitypolicyviolation', event => globalThis.uiCspViolations.push(event.violatedDirective));
});
const wait = predicate => page.waitForFunction(predicate, undefined, { timeout: 12000 });
const pause = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
const readingPosition = () => page.evaluate(() => {
  const root = document.querySelector('#history').getBoundingClientRect();
  const node = [...document.querySelectorAll('.record')].find(item => item.getBoundingClientRect().bottom > root.top && item.getBoundingClientRect().top < root.bottom);
  return { seq: Number(node.dataset.seq), offset: node.getBoundingClientRect().top - root.top };
});
try {
  await page.goto(origin);
  await wait(() => document.querySelectorAll('.record').length === 3);
  assert.equal(await page.locator('.record[data-seq="1"] .json').textContent(), JSON.stringify(snapshot, null, 2));
  assert.equal(await page.locator('.instruction').textContent(), original);
  assert.equal(await page.locator('.answer').textContent(), answer);
  assert.ok(answer.length > 4096);
  assert.equal(await page.locator('#records img, #records script').count(), 0);
  assert.equal(await page.evaluate(() => globalThis.uiInjected), undefined);
  assert.equal(await page.locator('#steer').isDisabled(), true);
  assert.equal(await page.locator('#interrupt').isDisabled(), true);
  assert.equal(await page.locator('#session-select option[value="broken"]').isDisabled(), true);
  assert.match(await page.locator('#session-select option[value="broken"]').textContent(), /読み込み不可/);
  assert.equal(await page.locator('#session-select option[value="broken"]').getAttribute('title'), 'metadata.json is missing');

  // Queue from idle; original text survives a pending request and later edits survive success.
  let release;
  postResponder = async (operation, body) => { await new Promise(resolve => { release = resolve; }); return accepted(operation, body); };
  await page.locator('#instruction').fill(original);
  assert.equal(await page.locator('#queue').isEnabled(), true);
  await page.locator('#queue').click();
  await wait(() => document.querySelector('#action-status').textContent.includes('送信中'));
  while (!release) await pause(10);
  assert.equal(posts.at(-1).body.instruction, original);
  assert.equal(posts.at(-1).origin, origin);
  assert.equal(posts.at(-1).contentType, 'application/json');
  assert.ok(posts.at(-1).body.commandId);
  assert.equal(await page.locator('#instruction').inputValue(), original);
  await page.locator('#instruction').fill('new draft while sending');
  release();
  await wait(() => document.querySelector('#action-status').textContent.includes('受付を記録'));
  assert.equal(await page.locator('#instruction').inputValue(), 'new draft while sending');
  assert.match(await page.locator('#action-status').textContent(), /反映・完了は/);
  console.log('PASS safe full text/JSON rendering, CSP, idle Queue and input retention');

  // An explicit failure retains the original input.
  postResponder = async () => ({ status: 409, data: { error: 'fixture rejection' } });
  await page.locator('#instruction').fill('keep on failure');
  await page.locator('#queue').click();
  await wait(() => document.querySelector('#action-status').textContent.includes('fixture rejection'));
  assert.equal(await page.locator('#instruction').inputValue(), 'keep on failure');

  // Run/command changes during in-flight operations must never retarget them.
  Object.assign(snapshot, { state: 'running', currentRunId: 'run-A', currentCommandId: 'command-A' });
  await wait(() => document.querySelector('#agent-state').textContent === 'running' && !document.querySelector('#steer').disabled);
  release = null;
  postResponder = async () => { await new Promise(resolve => { release = resolve; }); return { status: 409, data: { error: 'target ended' } }; };
  await page.locator('#instruction').fill(original);
  await page.locator('#steer').click();
  while (!release) await pause(10);
  assert.equal(posts.at(-1).body.runId, 'run-A');
  Object.assign(snapshot, { currentRunId: 'run-B', currentCommandId: 'command-B', queueLength: 2 });
  await wait(() => document.querySelector('#queue-length').textContent === '2');
  assert.equal(posts.at(-1).body.runId, 'run-A');
  release();
  await wait(() => document.querySelector('#action-status').textContent.includes('target ended') && !document.querySelector('#interrupt').disabled);
  assert.equal(await page.locator('#instruction').inputValue(), original);
  release = null;
  await page.locator('#interrupt').click();
  while (!release) await pause(10);
  assert.equal(posts.at(-1).body.commandId, 'command-B');
  Object.assign(snapshot, { currentRunId: 'run-C', currentCommandId: 'command-C' });
  release();
  await wait(() => document.querySelector('#action-status').textContent.includes('target ended') && !document.querySelector('#stop').disabled);
  assert.equal(posts.filter(post => post.operation === 'steer').length, 1);
  assert.equal(posts.filter(post => post.operation === 'interrupt').length, 1);
  postResponder = async (operation, body) => accepted(operation, body);
  await page.locator('#stop').click();
  await wait(() => document.querySelector('#action-status').textContent.includes('全停止 要求の受付'));
  assert.equal(await page.locator('#agent-state').textContent(), 'running');
  snapshot.state = 'stopped';
  await wait(() => document.querySelector('#agent-state').textContent === 'stopped');
  for (const operation of ['queue', 'steer', 'interrupt', 'stop']) assert.equal(await page.locator('#' + operation).isDisabled(), true);
  console.log('PASS failure retention, frozen Steer/Interrupt targets, receipt semantics, stopped controls');

  await page.locator('#session-select').selectOption('past');
  await wait(() => document.querySelectorAll('.record').length === 4 && document.querySelector('.unrecorded-result'));
  assert.equal(await page.locator('.unrecorded-result').count(), 1);
  assert.equal(await page.locator('.unrecorded-result').textContent(), 'セッション終了時点で結果未記録');
  assert.match(await page.locator('.record[data-type="observe"] h3').textContent(), /過去/);
  assert.match(await page.locator('#session-note').textContent(), /読み取り専用/);
  assert.equal(await page.locator('.answer').textContent(), answer);
  for (const operation of ['queue', 'steer', 'interrupt', 'stop']) assert.equal(await page.locator('#' + operation).isDisabled(), true);
  const pastRequestCount = historyRequests.filter(request => request.sessionId === 'past').length;
  Object.assign(snapshot, { state: 'running', queueLength: 7 });
  await wait(() => document.querySelector('#queue-length').textContent === '7');
  assert.equal(await page.locator('#agent-name').textContent(), 'worker');
  assert.equal(historyRequests.filter(request => request.sessionId === 'past').length, pastRequestCount);
  console.log('PASS read-only past sessions, explicit unresolved flag, historical snapshots, latest live header');

  // Load a large history through actual scroll paging and discard both edges.
  histories.live = Array.from({ length: 2200 }, (_, index) => ({ seq: index + 1, time: stamp, type: 'observe', snapshot: { agentId: 'agent-live', name: 'worker', state: index % 2 ? 'running' : 'idle', currentRunId: 'r-' + index, queueLength: 0 } }));
  await page.locator('#session-select').selectOption('live');
  await wait(() => document.querySelector('.record:last-child')?.dataset.seq === '2200');
  assert.equal(await page.locator('.record').count(), 200);
  for (let step = 0; step < 5; step++) {
    const before = Number(await page.locator('.record:first-child').getAttribute('data-seq'));
    await page.locator('#history').evaluate(node => { node.scrollTop = 0; });
    await page.waitForFunction(seq => Number(document.querySelector('.record:first-child').dataset.seq) < seq, before);
    await wait(() => document.querySelector('#history').getAttribute('aria-busy') === 'false');
    assert.ok(await page.locator('.record').count() <= 1000);
  }
  assert.equal(await page.locator('.record').count(), 1000);
  assert.equal(await page.locator('.record:last-child').getAttribute('data-seq'), '2000');
  const anchorBefore = await readingPosition();
  const firstBefore = Number(await page.locator('.record:first-child').getAttribute('data-seq'));
  // Explicit older button while reading near the older edge also retains the visible anchor.
  await page.locator('#load-older').click();
  await page.waitForFunction(seq => Number(document.querySelector('.record:first-child').dataset.seq) < seq, firstBefore);
  const anchorAfter = await readingPosition();
  assert.equal(anchorAfter.seq, anchorBefore.seq);
  assert.ok(Math.abs(anchorAfter.offset - anchorBefore.offset) < 2, JSON.stringify({ anchorBefore, anchorAfter }));
  assert.equal(await page.locator('.record').count(), 1000);
  const oldEnd = Number(await page.locator('.record:last-child').getAttribute('data-seq'));
  const discardedStart = Number(await page.locator('.record:first-child').getAttribute('data-seq'));
  assert.equal(await page.locator('#load-newer').isEnabled(), true);
  await page.locator('#load-newer').click();
  await page.waitForFunction(seq => Number(document.querySelector('.record:last-child').dataset.seq) > seq, oldEnd);
  const newerPosition = await readingPosition();
  assert.equal(newerPosition.seq, anchorAfter.seq);
  assert.ok(Math.abs(newerPosition.offset - anchorAfter.offset) < 2);
  assert.ok(Number(await page.locator('.record:first-child').getAttribute('data-seq')) > discardedStart);
  assert.equal(await page.locator('.record').count(), 1000);
  const oldPosition = await readingPosition();
  // Manual paging must progress even when its opposite-edge anchor is evicted.
  const paging = await browser.newPage();
  try {
    await paging.goto(origin);
    await paging.waitForFunction(() => document.querySelector('.record:last-child')?.dataset.seq === '2200');
    while (Number(await paging.locator('.record:first-child').getAttribute('data-seq')) > 1) {
      const first = Number(await paging.locator('.record:first-child').getAttribute('data-seq'));
      await paging.locator('#history').evaluate(node => { node.scrollTop = 0; });
      await paging.waitForFunction(seq => Number(document.querySelector('.record:first-child').dataset.seq) < seq, first);
      await paging.waitForFunction(() => document.querySelector('#history').getAttribute('aria-busy') === 'false');
    }
    await paging.locator('#history').evaluate(node => { node.scrollTop = 0; });
    await paging.locator('#load-newer').click();
    await paging.waitForFunction(() => document.querySelector('.record:last-child')?.dataset.seq === '1200');
    assert.equal(await paging.locator('.record:first-child').getAttribute('data-seq'), '201');
    assert.equal(await paging.locator('.record').count(), 1000);
    while (Number(await paging.locator('.record:last-child').getAttribute('data-seq')) < 2200) {
      const last = Number(await paging.locator('.record:last-child').getAttribute('data-seq'));
      await paging.locator('#load-newer').click();
      await paging.waitForFunction(seq => Number(document.querySelector('.record:last-child').dataset.seq) > seq, last);
    }
    await paging.locator('#history').evaluate(node => { node.scrollTop = node.scrollHeight; });
    await paging.locator('#load-older').click();
    await paging.waitForFunction(() => document.querySelector('.record:first-child')?.dataset.seq === '1001');
    assert.equal(await paging.locator('.record:last-child').getAttribute('data-seq'), '2000');
    assert.equal(await paging.locator('.record').count(), 1000);
  } finally {
    await paging.close();
  }
  console.log('PASS manual newer-at-top and older-at-bottom paging at the 1,000-record cap');
  append({ type: 'observe', snapshot: { ...snapshot, queueLength: 8 } });
  snapshot.queueLength = 8;
  await wait(() => document.querySelector('#new-count').textContent.includes('新着 1 件'));
  assert.deepEqual(await readingPosition(), oldPosition);
  await pause(1150);
  assert.match(await page.locator('#new-count').textContent(), /新着 1 件/);
  assert.equal(await page.locator('#queue-length').textContent(), '8');
  await page.locator('#go-tail').click();
  await wait(() => document.querySelector('.record:last-child')?.dataset.seq === '2201');
  assert.equal(await page.locator('#new-count').textContent(), '');
  append({ type: 'result', commandId: 'last-command', runId: 'last-run', state: 'completed', answer: 'tail follows' });
  await wait(() => document.querySelector('.record:last-child')?.dataset.seq === '2202');
  assert.ok(await page.locator('#history').evaluate(node => node.scrollHeight - node.clientHeight - node.scrollTop < 2));
  assert.ok(await page.locator('.record').count() <= 1000);
  assert.ok(historyRequests.every(request => request.cap <= 1000));
  console.log('PASS 2,200+ records, bounded DOM, discarded edges, scroll anchors, stable new count, tail follow');

  await page.locator('#instruction').fill('draft across reconnect');
  stateUnavailable = true;
  await wait(() => document.querySelector('#connection-status').textContent.includes('サーバーに接続できません'));
  assert.equal(await page.locator('#instruction').isDisabled(), true);
  append({ type: 'observe', snapshot: { ...snapshot, queueLength: 9 } });
  stateUnavailable = false;
  await wait(() => document.querySelector('.record:last-child')?.dataset.seq === '2203' && !document.querySelector('#instruction').disabled);
  assert.equal(await page.locator('#instruction').inputValue(), 'draft across reconnect');
  const reopened = await browser.newPage();
  reopened.on('pageerror', error => pageErrors.push(error.message));
  await reopened.goto(origin);
  await reopened.waitForFunction(() => document.querySelector('.record:last-child')?.dataset.seq === '2203');
  assert.equal(await reopened.locator('.answer').textContent(), 'tail follows');
  await reopened.close();
  console.log('PASS reconnect and reopened-browser history retrieval, retained draft');

  assert.deepEqual(pageErrors, []);
  assert.deepEqual(await page.evaluate(() => globalThis.uiCspViolations), []);
  await page.setViewportSize({ width: 390, height: 740 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  assert.ok(await page.locator('#instruction').isVisible());
  console.log('UI browser checks passed (Chromium ' + browser.version() + ').');
} finally {
  await browser.close();
  await new Promise(resolve => server.close(resolve));
}
