// Run with an existing Playwright installation; the console itself needs no npm dependencies.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs [NOE_BIN=/path/to/noe] node examples/agent-console/tests/ui-cli.mjs
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const repo = fileURLToPath(new URL('../../../', import.meta.url));
const binary = process.env.NOE_BIN ? resolve(process.env.NOE_BIN) : join(repo, 'noe');
const temporary = await mkdtemp(join(tmpdir(), 'noe-agent-console-ui-'));
const runtimeDir = join(temporary, 'runtime');
const dataDir = join(temporary, 'sessions');
const workDir = join(temporary, 'work');
const keyFile = join(temporary, 'fixture-key');
const secret = 'ui-cli-local-fixture-secret';
const newline = String.fromCharCode(10);
const instruction = ['  Unicode の全文を返してください。', '<img src=x onerror="globalThis.uiInjected=true"> <script>globalThis.uiInjected=true</script>  '].join(newline);
const answer = '日本語🌱/é/'.repeat(700) + newline + '<img src=x onerror="globalThis.uiInjected=true"> <script>globalThis.uiInjected=true</script>' + newline + '終端 sentinel 🌌';
assert.ok(Buffer.byteLength(answer, 'utf8') > 4096);
const providerRequests = [];
const fixtureErrors = [];
const pageErrors = [];
let browser;
let child;
let childDone;
let childExited = false;
let spawnError;
let stdout = '';
let stderr = '';
const pause = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));

// Only the upstream provider is a fixture. main.js serves every HTML/CSS/JS/API request.
const provider = createServer(async (req, res) => {
  try {
    assert.equal(req.method, 'POST');
    assert.ok(req.url.endsWith('/chat/completions'));
    assert.equal(req.headers.authorization, 'Bearer ' + secret);
    let body = '';
    for await (const chunk of req) body += chunk;
    const request = JSON.parse(body);
    providerRequests.push(request);
    assert.equal(providerRequests.length, 1, 'the one queued instruction should make one provider call');
    assert.equal(request.model, 'fixture/model');
    assert.ok(request.messages.some(message => message.role === 'user' && message.content === instruction));
    res.writeHead(200, { 'Content-Type': 'text/event-stream' });
    const chunk = (delta, finish_reason = null) => {
      res.write('data: ' + JSON.stringify({ id: 'ui-cli-fixture', object: 'chat.completion.chunk', created: 1, model: request.model, choices: [{ index: 0, delta, finish_reason }] }) + newline + newline);
    };
    chunk({ role: 'assistant', content: null });
    chunk({ content: answer });
    chunk({}, 'stop');
    res.write('data: ' + JSON.stringify({ choices: [], usage: { prompt_tokens: 100, completion_tokens: 100, total_tokens: 200 } }) + newline + newline);
    res.end('data: [DONE]' + newline + newline);
  } catch (error) {
    fixtureErrors.push(error.message);
    if (!res.headersSent) res.writeHead(500);
    res.end('local UI fixture failure');
  }
});

function observePage(page) {
  page.on('pageerror', error => pageErrors.push(error.message));
}

async function startupUrl() {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const prefix = 'NOE Agent Console: ';
    const line = stdout.split(newline).find(line => line.startsWith(prefix));
    if (line) {
      const url = new URL(line.slice(prefix.length).trim());
      assert.equal(url.protocol, 'http:');
      assert.equal(url.hostname, '127.0.0.1');
      assert.ok(url.port && url.port !== '0');
      return url.origin;
    }
    if (spawnError) throw spawnError;
    if (childExited) throw new Error('NOE exited before announcing its URL');
    await pause(25);
  }
  throw new Error('NOE startup did not announce its URL within 15 seconds');
}

try {
  await Promise.all([mkdir(runtimeDir), mkdir(dataDir), mkdir(workDir), writeFile(keyFile, secret + newline, { mode: 0o600 })]);
  await new Promise(resolve => provider.listen(0, '127.0.0.1', resolve));
  const providerUrl = 'http://127.0.0.1:' + provider.address().port;
  child = spawn(binary, [
    'run', join(repo, 'examples/agent-console/main.js'),
    '--provider', 'openrouter', '--model', 'fixture/model', '--effort', 'auto',
    '--port', '0', '--data-dir', dataDir,
  ], {
    cwd: workDir,
    env: { ...process.env, NOE_OPENROUTER_BASE_URL: providerUrl, NOE_OPENROUTER_API_KEY_FILE: keyFile, NOE_AGENT_RUNTIME_DIR: runtimeDir, NOE_AGENT_TRACE: '' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  child.stdout.setEncoding('utf8');
  child.stderr.setEncoding('utf8');
  child.stdout.on('data', data => { stdout = (stdout + data).slice(-65536); });
  child.stderr.on('data', data => { stderr = (stderr + data).slice(-65536); });
  childDone = new Promise(resolve => {
    child.once('error', error => { spawnError = error; childExited = true; resolve(); });
    child.once('exit', () => { childExited = true; resolve(); });
  });
  const origin = await startupUrl();
  browser = await chromium.launch({ headless: true, timeout: 15000 });
  const context = await browser.newContext({ viewport: { width: 1100, height: 850 } });
  context.setDefaultTimeout(15000);
  await context.addInitScript(() => {
    globalThis.uiCspViolations = [];
    document.addEventListener('securitypolicyviolation', event => globalThis.uiCspViolations.push(event.violatedDirective));
  });
  let page = await context.newPage();
  observePage(page);
  const assets = new Map();
  page.on('response', response => {
    const path = new URL(response.url()).pathname;
    if (['/app.js', '/timeline.js', '/style.css'].includes(path)) assets.set(path, response.status());
  });
  const entry = await page.goto(origin);
  assert.equal(entry.status(), 200);
  const csp = entry.headers()['content-security-policy'];
  for (const directive of ["script-src 'self'", "style-src 'self'", "connect-src 'self'", "frame-ancestors 'none'", "base-uri 'none'"]) assert.ok(csp.includes(directive));
  await page.waitForFunction(() => document.querySelector('#agent-state').textContent === 'idle' && document.querySelector('.record[data-type="observe"]'));
  assert.deepEqual([...assets.entries()].sort(), [['/app.js', 200], ['/style.css', 200], ['/timeline.js', 200]]);
  assert.equal(await page.evaluate(() => getComputedStyle(document.body).overflow), 'hidden');
  const sessionId = await page.locator('#session-select').inputValue();
  assert.ok(sessionId);
  // One normal observation/poll interval must not trigger inference from idle.
  await pause(1100);
  assert.equal(providerRequests.length, 0);
  console.log('PASS actual main.js entry, static ESM/CSS, CSP, idle without provider calls');

  await page.locator('#instruction').fill(instruction);
  const queuedResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/queue' && response.request().method() === 'POST');
  await page.locator('#queue').click();
  const queued = await queuedResponse;
  assert.equal(queued.status(), 200);
  assert.equal((await queued.request().allHeaders()).origin, origin);
  const acceptance = await queued.json();
  assert.equal(acceptance.receipt.status, 'accepted');
  assert.equal(acceptance.record.instruction, instruction);
  await page.waitForFunction(() => document.querySelector('.answer')?.textContent.endsWith('終端 sentinel 🌌'));
  assert.equal(await page.locator('.answer').textContent(), answer);
  assert.equal(await page.locator('.instruction').textContent(), instruction);
  assert.equal(await page.locator('#records img, #records script').count(), 0);
  assert.equal(await page.evaluate(() => globalThis.uiInjected), undefined);
  assert.deepEqual(await page.evaluate(() => globalThis.uiCspViolations), []);
  assert.deepEqual(fixtureErrors, []);
  assert.equal(providerRequests.length, 1);
  console.log('PASS real form Queue + browser Origin accepted, full Unicode answer (' + Buffer.byteLength(answer, 'utf8') + ' bytes), original HTML inert');

  await page.close();
  page = await context.newPage();
  observePage(page);
  await page.goto(origin);
  await page.waitForFunction(() => document.querySelector('.answer')?.textContent.endsWith('終端 sentinel 🌌'));
  assert.equal(await page.locator('#session-select').inputValue(), sessionId);
  assert.equal(await page.locator('.answer').textContent(), answer);
  assert.equal(await page.locator('.instruction').textContent(), instruction);
  assert.equal(await page.locator('#records img, #records script').count(), 0);
  console.log('PASS closing/reopening browser page retains actual session and history');

  const stoppedResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/stop' && response.request().method() === 'POST');
  await page.locator('#stop').click();
  const stopped = await stoppedResponse;
  assert.equal(stopped.status(), 200);
  assert.equal((await stopped.request().allHeaders()).origin, origin);
  await page.waitForFunction(() => document.querySelector('#agent-state').textContent === 'stopped');
  for (const operation of ['queue', 'steer', 'interrupt', 'stop']) assert.equal(await page.locator('#' + operation).isDisabled(), true);
  assert.equal(await page.locator('.answer').textContent(), answer);
  assert.equal(providerRequests.length, 1);
  assert.deepEqual(fixtureErrors, []);
  assert.deepEqual(pageErrors, []);
  assert.deepEqual(await page.evaluate(() => globalThis.uiCspViolations), []);
  console.log('PASS actual all-stop shows stopped and preserves the result');
  console.log('UI CLI smoke passed (Chromium ' + browser.version() + ', NOE ' + binary + ').');
} catch (error) {
  console.error('NOE stdout:', stdout);
  console.error('NOE stderr:', stderr);
  if (fixtureErrors.length) console.error('Provider fixture:', fixtureErrors);
  throw error;
} finally {
  if (browser) await browser.close();
  if (child && !childExited) {
    child.kill('SIGINT');
    await Promise.race([childDone, pause(2000)]);
    if (!childExited) {
      child.kill('SIGKILL');
      await Promise.race([childDone, pause(2000)]);
    }
  }
  provider.closeAllConnections();
  await new Promise(resolve => provider.close(resolve));
  await rm(temporary, { recursive: true, force: true });
}
