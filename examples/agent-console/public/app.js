import { actionRequest, availableActions, mergeRecords, windowSize } from './timeline.js';

const byId = id => document.getElementById(id);
const ui = Object.fromEntries([
  'agent-name', 'agent-state', 'queue-length', 'agent-id', 'agent-model',
  'connection-status', 'session-select', 'session-note', 'history-range',
  'load-older', 'load-newer', 'go-tail', 'new-count', 'history-error',
  'history', 'history-empty', 'records', 'instruction-form', 'instruction',
  'queue', 'steer', 'interrupt', 'stop', 'action-help', 'action-status',
].map(id => [id, byId(id)]));

let state = null;
let connected = false;
let sessions = [];
let sessionOptionsKey = '';
let selectedSessionId = null;
let records = [];
let total = 0;
let acknowledgedTotal = 0;
let limit = 1000;
let pollInterval = 1000;
let following = true;
let historyReady = false;
let historyBusy = false;
let historyNeedsRefresh = false;
let generation = 0;
let pendingAction = false;
let pollPromise = null;
let suppressScroll = false;
let scrollRevision = 0;
// Only nodes in the bounded window are retained here.
let recordNodes = new Map();

const isPast = () => Boolean(selectedSessionId && state && selectedSessionId !== state.sessionId);
const firstSeq = () => records[0]?.seq || 0;
const lastSeq = () => records[records.length - 1]?.seq || 0;
const nearEnd = () => ui.history.scrollHeight - ui.history.clientHeight - ui.history.scrollTop < 96;
const atLatestPage = () => lastSeq() >= total;

function message(element, text) {
  element.textContent = text || '';
  element.hidden = !text;
}

async function api(path, options = {}) {
  const response = await fetch(path, { cache: 'no-store', credentials: 'same-origin', ...options });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || ('HTTP ' + response.status));
  return data;
}

function historyUrl(sessionId, pageLimit, edge = {}) {
  const params = new URLSearchParams({ session: sessionId, limit: String(Math.min(1000, pageLimit)) });
  for (const [key, value] of Object.entries(edge)) params.set(key, String(value));
  return '/api/history?' + params;
}

function captureAnchor() {
  const bounds = ui.history.getBoundingClientRect();
  for (const node of ui.records.children) {
    const rect = node.getBoundingClientRect();
    if (rect.bottom > bounds.top && rect.top < bounds.bottom) {
      return { seq: Number(node.dataset.seq), offset: rect.top - bounds.top, scrollTop: ui.history.scrollTop };
    }
  }
  return { scrollTop: ui.history.scrollTop };
}

function restoreAnchor(anchor, follow) {
  suppressScroll = true;
  const revision = ++scrollRevision;
  if (follow) {
    ui.history.scrollTop = ui.history.scrollHeight;
  } else {
    const node = recordNodes.get(anchor?.seq)?.node;
    if (node) {
      const offset = node.getBoundingClientRect().top - ui.history.getBoundingClientRect().top;
      ui.history.scrollTop += offset - anchor.offset;
    } else if (anchor) {
      ui.history.scrollTop = anchor.scrollTop;
    }
  }
  requestAnimationFrame(() => {
    if (revision === scrollRevision) suppressScroll = false;
  });
}

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = String(text);
  return node;
}

function textBlock(parent, label, value, className = '') {
  if (label) parent.append(element('p', 'record-label', label));
  parent.append(element('pre', className, value ?? ''));
}

function jsonBlock(parent, label, value) {
  textBlock(parent, label, JSON.stringify(value, null, 2), 'json');
}

function renderRecord(record, historical) {
  const node = element('li', 'record');
  node.dataset.seq = String(record.seq);
  node.dataset.type = record.type;
  const header = element('div', 'record-header');
  const time = element('time');
  const parsedTime = new Date(record.time);
  time.textContent = Number.isNaN(parsedTime.getTime()) ? String(record.time) : parsedTime.toLocaleString();
  if (!Number.isNaN(parsedTime.getTime())) time.dateTime = parsedTime.toISOString();
  time.title = String(record.time);
  const operationNames = { queue: 'Queue', steer: 'Steer', interrupt: 'Interrupt', stop: '全停止' };
  let title = record.type;
  if (record.type === 'observe') title = historical ? 'Observe（過去の記録）' : 'Observe';
  if (record.type === 'operation') title = (operationNames[record.operation] || record.operation) + (record.error ? ' 要求エラー' : ' 要求・受付記録');
  if (record.type === 'result') title = '結果 — ' + record.state;
  if (record.type === 'error') title = 'エラー — ' + record.source;
  header.append(time, element('h3', '', title), element('span', 'record-seq', '#' + record.seq));
  node.append(header);

  if (record.type === 'observe') {
    if (historical) node.append(element('p', 'record-label', '記録時点の snapshot。現在稼働中の agent を示すものではありません。'));
    jsonBlock(node, '', record.snapshot);
  } else if (record.type === 'operation') {
    if (record.instruction !== undefined) textBlock(node, '元の指示', record.instruction, 'instruction');
    jsonBlock(node, '対象・受付内容（受付は反映・完了を意味しません）', {
      target: record.target,
      commandId: record.commandId,
      receipt: record.receipt,
    });
    if (record.error) textBlock(node, 'エラー', record.error, 'error');
    if (historical && record.operation === 'queue' && record.resultRecorded === false) {
      node.append(element('p', 'notice error unrecorded-result', 'セッション終了時点で結果未記録'));
    }
  } else if (record.type === 'result') {
    jsonBlock(node, '結果の対象', { commandId: record.commandId, runId: record.runId, state: record.state });
    textBlock(node, '最終回答（全文）', record.answer, 'answer');
    if (record.error) textBlock(node, 'エラー', record.error, 'error');
    if (record.errorDetails !== undefined) jsonBlock(node, 'エラー詳細', record.errorDetails);
  } else if (record.type === 'error') {
    textBlock(node, '記録されたエラー', typeof record.error === 'string' ? record.error : JSON.stringify(record.error, null, 2), 'error');
  } else {
    jsonBlock(node, '', record);
  }
  return node;
}

function renderRecords(anchor, follow = false) {
  const historical = isPast();
  const nextNodes = new Map();
  const retained = new Set(records.map(record => record.seq));
  for (const [seq, entry] of recordNodes) {
    if (!retained.has(seq)) entry.node.remove();
  }
  let cursor = ui.records.firstChild;
  for (const record of records) {
    let entry = recordNodes.get(record.seq);
    if (!entry || entry.record !== record || entry.historical !== historical) {
      const node = renderRecord(record, historical);
      if (entry) {
        if (cursor === entry.node) cursor = entry.node.nextSibling;
        entry.node.replaceWith(node);
        cursor = node;
      }
      entry = { node, record, historical };
    }
    if (entry.node === cursor) cursor = cursor.nextSibling;
    else ui.records.insertBefore(entry.node, cursor);
    nextNodes.set(record.seq, entry);
  }
  recordNodes = nextNodes;
  ui['history-empty'].hidden = records.length > 0;
  ui['history-empty'].textContent = historyReady ? 'このセッションの履歴はまだありません。' : '履歴を読み込み中…';
  restoreAnchor(anchor, follow);
}

function renderHeader() {
  const snapshot = state?.snapshot;
  ui['agent-name'].textContent = snapshot?.name || 'agent';
  ui['agent-id'].textContent = snapshot?.agentId ? 'Agent: ' + snapshot.agentId : '';
  ui['agent-state'].textContent = snapshot?.state || '状態未取得';
  ui['agent-state'].dataset.state = connected ? (snapshot?.state || '') : '';
  ui['queue-length'].textContent = snapshot?.queueLength ?? '—';
  const model = state?.model;
  ui['agent-model'].textContent = model ? 'Model: ' + [model.provider, model.model, model.effort].filter(Boolean).join(' / ') : 'Model: —';
}

function renderSessions() {
  const options = sessions.slice();
  if (state && !options.some(session => session.sessionId === state.sessionId)) {
    options.unshift({ sessionId: state.sessionId, name: state.snapshot?.name || 'agent' });
  }
  if (selectedSessionId && !options.some(session => session.sessionId === selectedSessionId)) {
    options.push({ sessionId: selectedSessionId });
  }
  const key = JSON.stringify([state?.sessionId, options]);
  if (key !== sessionOptionsKey) {
    const nodes = options.map(session => {
      const option = element('option');
      option.value = session.sessionId;
      option.disabled = Boolean(session.error);
      if (session.error) option.title = session.error;
      const started = session.startedAt ? new Date(session.startedAt).toLocaleString() : '';
      option.textContent = [session.name, started, session.sessionId].filter(Boolean).join(' · ') + (session.sessionId === state?.sessionId ? '（現在）' : '（過去・閲覧専用）');
      return option;
    });
    ui['session-select'].replaceChildren(...nodes);
    sessionOptionsKey = key;
  }
  if (selectedSessionId) ui['session-select'].value = selectedSessionId;
  ui['session-select'].disabled = options.length === 0;
  ui['session-note'].textContent = isPast()
    ? '過去セッション・読み取り専用。Observe は記録時点の状態であり、agent やキューは再開されません。上のヘッダーは現在の agent です。'
    : '現在のセッション。同じ agent の各 run の Observe・操作・結果を時系列で表示します。';
  ui['session-note'].classList.toggle('historical', isPast());
}

function renderControls() {
  const readOnly = !connected || isPast() || !state;
  const actions = availableActions(state?.snapshot, readOnly, pendingAction);
  const hasInstruction = ui.instruction.value.trim().length > 0;
  ui.queue.disabled = !actions.queue || !hasInstruction;
  ui.steer.disabled = !actions.steer || !hasInstruction;
  ui.interrupt.disabled = !actions.interrupt;
  ui.stop.disabled = !actions.stop;
  // Edits are allowed while a request is pending and will not be erased on receipt.
  ui.instruction.disabled = !availableActions(state?.snapshot, readOnly).queue;
  ui.instruction.placeholder = isPast() ? '過去セッションは閲覧専用です' : '指示を入力…';
  ui['load-older'].disabled = historyBusy || !historyReady || firstSeq() <= 1;
  ui['load-newer'].disabled = historyBusy || !historyReady || atLatestPage();
  ui['go-tail'].disabled = historyBusy || !selectedSessionId || (historyReady && following && atLatestPage());
  const newCount = isPast() ? 0 : Math.max(0, total - acknowledgedTotal);
  ui['new-count'].textContent = newCount > 0 ? '新着 ' + newCount + ' 件 / ' : '';
  ui['go-tail'].classList.toggle('has-new', newCount > 0);
  ui['history-range'].textContent = historyReady
    ? (records.length ? '#' + firstSeq() + '–#' + lastSeq() + ' / 全 ' + total + ' 件' : '全 0 件') + '（表示上限 ' + limit + ' 件・範囲外は再読込）'
    : '履歴を読み込み中…';
  ui.history.setAttribute('aria-busy', String(historyBusy));
}

async function loadHistory(direction, { jump = false, manual = false } = {}) {
  if (historyBusy || !selectedSessionId) return;
  const requestGeneration = generation;
  const sessionId = selectedSessionId;
  const edge = {};
  let pageLimit = Math.min(200, limit);
  let replace = direction === 'latest' || direction === 'refresh';
  if (direction === 'older') edge.before = firstSeq();
  if (direction === 'newer') edge.after = lastSeq();
  if (direction === 'refresh' && records.length) {
    edge.after = firstSeq() - 1;
    pageLimit = records.length;
  }
  if (direction === 'tail') {
    pageLimit = limit;
    if (total - lastSeq() > limit) replace = true;
    else edge.after = lastSeq();
  }
  historyBusy = true;
  renderControls();
  try {
    const page = await api(historyUrl(sessionId, pageLimit, edge));
    if (requestGeneration !== generation) return;
    total = Math.max(total, page.total);
    const follow = jump || (direction === 'tail' && following && nearEnd());
    // A user may have scrolled away while this tail request was in flight.
    if (direction === 'tail' && !follow) return;
    const anchor = captureAnchor();
    records = replace ? page.records.slice(-limit) : mergeRecords(records, page.records, {
      limit, direction: direction === 'older' ? 'older' : 'newer', anchorSeq: follow || manual ? undefined : anchor.seq,
    });
    historyReady = true;
    if (direction === 'refresh') historyNeedsRefresh = false;
    following = follow;
    // Manual paging must retain the requested page even if that evicts the
    // visible anchor. Automatic scrolling continues to preserve that anchor.
    const position = manual && page.records.length && !records.some(record => record.seq === anchor.seq)
      ? { seq: direction === 'older' ? page.lastSeq : page.firstSeq, offset: 0, scrollTop: anchor.scrollTop }
      : anchor;
    renderRecords(position, follow);
    if (follow || (atLatestPage() && nearEnd())) {
      following = true;
      acknowledgedTotal = total;
    }
    message(ui['history-error'], '');
  } catch (error) {
    if (requestGeneration === generation) {
      message(ui['history-error'], '履歴の取得に失敗しました: ' + error.message + '。ページ操作または「末尾へ」で再取得できます。');
      if (!records.length) ui['history-empty'].textContent = '履歴を取得できませんでした。';
    }
  } finally {
    if (requestGeneration === generation) {
      historyBusy = false;
      renderControls();
    }
  }
}

async function selectSession(sessionId) {
  generation++;
  selectedSessionId = sessionId;
  records = [];
  total = 0;
  acknowledgedTotal = 0;
  historyReady = false;
  historyBusy = false;
  historyNeedsRefresh = false;
  following = !isPast();
  message(ui['history-error'], '');
  renderSessions();
  renderRecords(null);
  renderControls();
  await loadHistory('latest', { jump: true });
}

async function pollHistory() {
  if (!selectedSessionId || historyBusy) return;
  if (!historyReady) return loadHistory('latest', { jump: true });
  if (isPast()) {
    if (historyNeedsRefresh) await loadHistory('refresh');
    return;
  }
  const requestGeneration = generation;
  const page = await api(historyUrl(selectedSessionId, 1, { after: total }));
  if (requestGeneration !== generation) return;
  // The server's total is authoritative; repeated polls cannot double-count arrivals.
  total = Math.max(total, page.total);
  if (following && nearEnd() && lastSeq() < total) await loadHistory('tail');
  renderControls();
}

async function refresh() {
  if (pollPromise) return pollPromise;
  pollPromise = (async () => {
    const [stateResult, sessionsResult] = await Promise.allSettled([api('/api/state'), api('/api/sessions')]);
    if (sessionsResult.status === 'fulfilled') sessions = sessionsResult.value.sessions;
    if (stateResult.status === 'fulfilled') {
      const previousSessionId = state?.sessionId;
      state = stateResult.value;
      connected = true;
      pollInterval = Number.isFinite(state.pollInterval) && state.pollInterval > 0 ? state.pollInterval : 1000;
      const newLimit = windowSize(state.windowSize);
      if (newLimit !== limit) {
        limit = newLimit;
        const anchor = captureAnchor();
        records = mergeRecords(records, [], { limit, anchorSeq: anchor.seq });
        renderRecords(anchor, following);
      }
      if (previousSessionId && previousSessionId !== state.sessionId && selectedSessionId === previousSessionId) {
        following = false;
        historyNeedsRefresh = true;
        renderRecords(captureAnchor());
      }
      message(ui['connection-status'], state.error ? '監視エラー: ' + state.error : '');
      renderHeader();
      renderSessions();
      renderControls();
      if (!selectedSessionId) await selectSession(state.sessionId);
      else {
        try { await pollHistory(); }
        catch (error) { message(ui['history-error'], '履歴の更新に失敗しました: ' + error.message); }
      }
    } else {
      connected = false;
      message(ui['connection-status'], 'サーバーに接続できません。ヘッダーは最後の取得状態です: ' + stateResult.reason.message);
      renderHeader();
      renderSessions();
      renderControls();
    }
    if (sessionsResult.status === 'rejected' && stateResult.status === 'fulfilled') {
      message(ui['connection-status'], 'セッション一覧の取得に失敗しました: ' + sessionsResult.reason.message);
    }
  })();
  try { await pollPromise; }
  finally { pollPromise = null; }
}

async function sendAction(operation) {
  const actions = availableActions(state?.snapshot, !connected || isPast() || !state, pendingAction);
  if (!actions[operation]) return;
  const instruction = ui.instruction.value;
  if ((operation === 'queue' || operation === 'steer') && !instruction.trim()) return;
  // Freeze session, run and command IDs BEFORE the asynchronous network request.
  const body = actionRequest(operation, {
    sessionId: state.sessionId, snapshot: state.snapshot, instruction,
    commandId: (operation === 'queue' || operation === 'steer') && globalThis.crypto?.randomUUID ? crypto.randomUUID() : undefined,
  });
  pendingAction = true;
  renderControls();
  const label = { queue: 'Queue', steer: 'Steer', interrupt: 'Interrupt', stop: '全停止' }[operation];
  ui['action-status'].classList.remove('error');
  ui['action-status'].textContent = label + ' 要求を送信中…';
  try {
    await api('/api/' + operation, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
    });
    if ((operation === 'queue' || operation === 'steer') && ui.instruction.value === instruction) ui.instruction.value = '';
    ui['action-status'].textContent = label + ' 要求の受付を記録しました。反映・完了は最新状態と履歴で確認してください。';
    await refresh();
  } catch (error) {
    ui['action-status'].classList.add('error');
    ui['action-status'].textContent = label + ' 要求: ' + error.message + '。入力は保持しています。通信エラーの場合は、再送前に履歴で受付の有無を確認してください。';
    await refresh();
  } finally {
    pendingAction = false;
    renderControls();
  }
}

ui['session-select'].addEventListener('change', () => { void selectSession(ui['session-select'].value); });
ui['load-older'].addEventListener('click', () => { following = false; void loadHistory('older', { manual: true }); });
ui['load-newer'].addEventListener('click', () => { following = false; void loadHistory('newer', { manual: true }); });
ui['go-tail'].addEventListener('click', () => { void loadHistory('latest', { jump: true }); });
ui.history.addEventListener('scroll', () => {
  if (suppressScroll || !historyReady) return;
  following = atLatestPage() && nearEnd();
  if (following) acknowledgedTotal = total;
  renderControls();
  if (historyBusy) return;
  if (ui.history.scrollTop < 120 && firstSeq() > 1) void loadHistory('older');
  else if (nearEnd() && !atLatestPage()) void loadHistory('newer');
}, { passive: true });
ui.instruction.addEventListener('input', renderControls);
ui['instruction-form'].addEventListener('submit', event => { event.preventDefault(); void sendAction('queue'); });
for (const operation of ['steer', 'interrupt', 'stop']) {
  ui[operation].addEventListener('click', () => { void sendAction(operation); });
}

async function tick() {
  try { await refresh(); }
  finally { setTimeout(tick, pollInterval); }
}
void tick();
