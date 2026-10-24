#!/usr/bin/env python3
"""Optional real-browser test. Requires Firefox (Marionette); no WebDriver package.
Run: python3 examples/agent-server/test_browser.py
NOE_BIN and FIREFOX_BIN override the executables. Uses the local SSE fixture.
"""
import argparse, json, socket, subprocess, tempfile, os, signal, pathlib, sys
sys.dont_write_bytecode = True
import test_server as f

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--without-events', action='store_true', help='compatibility startup option; the wrapper still enables detailed worker events')
    args = parser.parse_args()
    h = f.AgentServerTests()
    h.setUp()
    browser = None
    sock = None
    try:
        if args.without_events:
            h.start(events=False)
        else:
            h.start()
        with tempfile.TemporaryDirectory(prefix='noe-firefox-') as d:
            probe = socket.socket()
            probe.bind(('127.0.0.1', 0))
            port = probe.getsockname()[1]
            probe.close()
            pathlib.Path(d, 'user.js').write_text('user_pref("marionette.port",' + str(port) + ');\nuser_pref("browser.shell.checkDefaultBrowser",false);')
            log = open(d + '/firefox.log', 'w+')
            browser = subprocess.Popen([os.environ.get('FIREFOX_BIN', 'firefox'), '--headless', '--no-remote', '--marionette', '--profile', d], stdout=log, stderr=log, start_new_session=True)

            def connect():
                nonlocal sock
                try:
                    sock = socket.create_connection(('127.0.0.1', port), timeout=1)
                    return True
                except OSError:
                    return False
            try:
                f.eventually(connect, 'Firefox Marionette', timeout=25)
            except Exception:
                log.seek(0)
                print(log.read())
                raise
            sock.settimeout(20)

            def receive():
                size = b''
                while not size.endswith(b':'):
                    chunk = sock.recv(1)
                    if not chunk:
                        raise EOFError('Marionette disconnected')
                    size += chunk
                n = int(size[:-1])
                body = b''
                while len(body) < n:
                    chunk = sock.recv(n - len(body))
                    if not chunk:
                        raise EOFError('Marionette response truncated')
                    body += chunk
                return json.loads(body)
            print('browser handshake', receive())
            seq = 0

            def command(name, params={}):
                nonlocal seq
                seq += 1
                data = json.dumps([0, seq, name, params]).encode()
                sock.sendall(str(len(data)).encode() + b':' + data)
                result = receive()
                if result[2]:
                    raise AssertionError(result)
                return result[3]

            def js(script):
                # Use the page realm: a named Marionette sandbox has its own
                # window.fetch, so replacing it does not intercept UI requests.
                return command('WebDriver:ExecuteScript', {'script': script, 'args': [], 'newSandbox': False, 'sandbox': None})['value']

            def wait(script, desc):
                try:
                    return f.eventually(lambda: js(script), desc, timeout=20)
                except Exception:
                    print('browser diagnostics for', desc, js('return {requests:window.fixtureRequests,release:typeof window.fixtureRelease,queueDisabled:document.getElementById("submit-task").disabled,pageApi:typeof window.api,fetch:String(window.fetch),tasks:document.getElementById("task-list").textContent,actionError:document.getElementById("action-error").textContent,historyError:document.getElementById("history-error").textContent};'))
                    raise
            command('WebDriver:NewSession', {'capabilities': {'alwaysMatch': {'acceptInsecureCerts': False}}})
            command('WebDriver:Navigate', {'url': h.startup['url']})
            wait('return document.getElementById("agent-status").textContent.includes("idle")', 'initial poll')
            # Both normal startup and the compatibility option run --observe --events.
            wait('return document.getElementById("history").textContent.includes("agent idle")', 'initial observe history')
            assert js('return document.getElementById("history-mode").textContent.includes("Captured CLI --observe");')
            assert js('return !document.getElementById("history-setup");')
            assert js('return Array.from(document.querySelectorAll("section > h2")).map(h=>h.textContent);') == ['History', 'Tasks', 'Create a task']
            assert js('return document.querySelectorAll("textarea").length===1 && document.querySelectorAll("#task-form .controls button").length===4;')
            assert js('return Array.from(document.querySelectorAll("#task-form .controls button")).map(b=>b.textContent);') == ['Queue task', 'Steer', 'Stop / Cancel task', 'Refresh now']
            assert js('return !["detail-heading","detail","selection-hint","task-meta","task-instruction","progress","answer","task-error","result","steers","steer-form","steer-instruction"].some(id=>document.getElementById(id));')
            assert js('return document.getElementById("submit-steer").disabled && document.getElementById("interrupt").disabled;')
            assert js('return document.documentElement.lang;') == 'en'
            assert js('return document.title;') == 'NOE Agent Server'
            assert js('return document.getElementById("instruction").placeholder;') == 'Example: Summarize the key points of README.md in English.'

            # Keep real requests, recording paths and optionally delaying a POST
            # receipt so editing while an action is in flight is deterministic.
            # Optional stderr includes diagnostics, not necessarily errors.
            js('''
                window.fixtureRequests = [];
                window.fixtureStderr = null;
                const realFetch = window.fetch.bind(window);
                window.fetch = async (input, options = {}) => {
                    const path = new URL(input, location.href).pathname;
                    const method = options.method || 'GET';
                    window.fixtureRequests.push([method, path]);
                    const response = await realFetch(input, options);
                    if (path === '/history' && window.fixtureStderr !== null) {
                        const body = await response.json();
                        return new Response(JSON.stringify({...body, stderr: window.fixtureStderr, ...window.fixtureHistoryExtra}),
                            {status: response.status, headers: {'Content-Type': 'application/json'}});
                    }
                    if (method === 'POST' && path === window.fixtureDelayPath) {
                        window.fixtureDelayPath = null;
                        await new Promise(resolve => { window.fixtureRelease = resolve; });
                        window.fixtureRelease = null;
                    }
                    return response;
                };
                return true;
            ''')

            def queue(instruction):
                js('document.getElementById("instruction").value=' + json.dumps(instruction) + ';document.getElementById("task-form").requestSubmit();return true;')

            def selected(instruction, state):
                wait('const b=document.querySelector(\'#task-list button[aria-current="true"]\');return b && b.textContent.includes(' + json.dumps(instruction) + ') && document.getElementById("selected-task").textContent.includes(' + json.dumps('(' + state + ')') + ');', 'selected ' + instruction + ' ' + state)
                return js('return document.querySelector(\'#task-list button[aria-current="true"]\').textContent.split(" · ")[0];')

            def select_task(task_id):
                js('const b=Array.from(document.querySelectorAll("#task-list button")).find(b=>b.textContent.startsWith(' + json.dumps(task_id + ' · ') + '));if(!b)throw new Error("missing task");b.click();return true;')

            js('document.getElementById("instruction").value="draft-survives-auto-refresh";return true;')
            external = h.submit('external-auto')
            h.finished(external, answer='answer:external-auto')
            wait('return document.getElementById("task-list").textContent.includes("external-auto") && document.getElementById("history").textContent.includes("answer:external-auto")', 'automatic list/history refresh without clicking')
            assert js('return document.getElementById("instruction").value;') == 'draft-survives-auto-refresh'
            assert js('return document.getElementById("poll-status").textContent.includes("every 5 seconds");')
            select_task(external['id'])
            h.finished(h.submit('external-manual'), answer='answer:external-manual')
            js('document.getElementById("refresh").click();return true;')
            f.eventually(lambda: js('return document.getElementById("task-list").textContent.includes("external-manual") && document.getElementById("history").textContent.includes("answer:external-manual")'), 'manual list/history refresh', timeout=3)
            assert selected('external-auto', 'completed') == external['id']
            assert js('return document.getElementById("instruction").value;') == 'draft-survives-auto-refresh'

            first_tool_sent = False

            def first_reply(r):
                nonlocal first_tool_sent
                if f.latest_user(r) == 'browser-first' and not first_tool_sent:
                    first_tool_sent = True
                    return {'deltas': [{'reasoning': 'browser-thinking'}, {'content': 'browser-progress-message'}, f.tool('// Browser history fixture.\nconsole.log("browser tool output");')], 'finish': 'tool_calls'}
                return {'content': 'answer:' + f.latest_user(r)}
            h.provider.reply = first_reply
            js('window.fixtureDelayPath="/tasks";return true;')
            queue('browser-first')
            wait('return typeof window.fixtureRelease==="function" && document.getElementById("submit-task").disabled;', 'in-flight queue receipt')
            js('document.getElementById("instruction").value="draft-during-queue";window.fixtureRelease();return true;')
            first_id = selected('browser-first', 'completed')
            wait('return document.getElementById("history").textContent.includes("answer:browser-first") && document.getElementById("history").textContent.includes("run_js started: Browser history fixture.") && document.getElementById("history").textContent.includes("thinking: browser-thinking") && document.getElementById("history").textContent.includes("browser-progress-message");', 'captured tools, thinking, messages and answer')
            assert js('return document.getElementById("instruction").value;') == 'draft-during-queue'
            assert js('return document.getElementById("action-status").textContent;') == 'Task accepted (not yet completed).'
            assert js('return document.getElementById("submit-steer").disabled && document.getElementById("interrupt").disabled;')

            bad = '<img src=x onerror="window.fixtureXSS=true">'
            queue(bad)
            selected(bad, 'completed')
            wait('return document.getElementById("history").textContent.includes(' + json.dumps('answer:' + bad) + ');', 'safe history answer')
            assert js('return !window.fixtureXSS && document.getElementById("history").children.length===0 && Array.from(document.querySelectorAll("#task-list button")).every(b=>b.children.length===0);')
            js('window.fixtureStderr=' + json.dumps('stderr: ' + bad) + ';document.getElementById("refresh").click();return true;')
            wait('return !document.getElementById("history-stderr-section").hidden && document.getElementById("history-stderr").textContent===' + json.dumps('stderr: ' + bad) + ';', 'distinct worker stderr')
            assert js('return !window.fixtureXSS && document.getElementById("history-stderr").children.length===0 && !document.getElementById("history").textContent.includes("stderr: ");')
            js('window.fixtureStderr=null;document.getElementById("refresh").click();return true;')
            wait('return !document.getElementById("history-stderr").textContent.includes("<img");', 'stderr diagnostic restored on refresh')

            # Capture errors and a dead worker must not erase already captured history.
            js('window.fixtureHistoryExtra={error:"capture warning <unsafe>",workerStatus:{code:1,signal:null}};window.fixtureStderr="diagnostic";document.getElementById("refresh").click();return true;')
            wait('return document.getElementById("history-error").textContent.includes("capture warning <unsafe>") && document.getElementById("history-error").textContent.includes("Worker exited unexpectedly");', 'capture and worker-exit warning')
            assert js('return document.getElementById("history-error").children.length===0 && document.getElementById("history").textContent.includes("answer:browser-first");')
            js('window.fixtureHistoryExtra={};window.fixtureStderr=null;document.getElementById("refresh").click();return true;')
            wait('return document.getElementById("history-error").textContent==="";', 'capture warning cleared')

            gate = h.provider.gate()
            started = __import__('threading').Event()

            def reply(r):
                if f.latest_user(r) == 'browser-hold':
                    started.set()
                    h.provider.wait(gate)
                return {'content': 'answer:' + f.latest_user(r)}
            h.provider.reply = reply
            queue('browser-hold')
            assert started.wait(10)
            hold_id = selected('browser-hold', 'running')
            assert js('return !document.getElementById("submit-steer").disabled && !document.getElementById("interrupt").disabled;')
            js('window.fixtureDelayPath=' + json.dumps('/tasks/' + hold_id + '/steer') + ';document.getElementById("instruction").value="browser-correction";document.getElementById("submit-steer").click();return true;')
            wait('return typeof window.fixtureRelease==="function" && document.getElementById("submit-steer").disabled;', 'in-flight steer receipt')
            js('document.getElementById("instruction").value="draft-during-steer";window.fixtureRelease();return true;')
            wait('return document.getElementById("action-status").textContent.includes("Acceptance does not confirm application");', 'steer receipt is not application')
            wait('return document.getElementById("history").textContent.includes("steer accepted:");', 'steer acceptance in History')
            assert js('return !document.getElementById("history").textContent.includes("steer applied:");')
            assert js('return document.getElementById("instruction").value;') == 'draft-during-steer'
            gate.set()
            selected('browser-hold', 'completed')
            wait('return document.getElementById("history").textContent.includes("answer:browser-correction") && document.getElementById("history").textContent.includes("steer applied:");', 'exact steer lifecycle and answer in History')
            assert js('return document.getElementById("instruction").value;') == 'draft-during-steer'

            stop = h.provider.gate()
            stopping = __import__('threading').Event()

            def reply2(r):
                if f.latest_user(r) == 'browser-stop':
                    stopping.set()
                    h.provider.wait(stop)
                return {'content': 'answer:' + f.latest_user(r)}
            h.provider.reply = reply2
            queue('browser-stop')
            assert stopping.wait(10)
            stop_id = selected('browser-stop', 'running')
            queue('browser-queued-cancel')
            queued_id = selected('browser-queued-cancel', 'queued')
            assert js('return document.getElementById("submit-steer").disabled && !document.getElementById("interrupt").disabled;')
            js('document.getElementById("instruction").value="shared-draft-across-tasks";return true;')
            select_task(first_id)
            selected('browser-first', 'completed')
            assert js('return document.getElementById("instruction").value;') == 'shared-draft-across-tasks'
            assert js('return document.getElementById("submit-steer").disabled && document.getElementById("interrupt").disabled;')
            select_task(stop_id)
            assert js('return document.getElementById("instruction").value;') == 'shared-draft-across-tasks'
            assert js('return !document.getElementById("submit-steer").disabled && !document.getElementById("interrupt").disabled;')
            select_task(queued_id)
            js('document.getElementById("interrupt").click();return true;')
            selected('browser-queued-cancel', 'cancelled')
            assert js('return document.getElementById("action-status").textContent.includes("request accepted") && document.getElementById("action-status").textContent.includes("confirm completion");')
            assert js('return document.getElementById("instruction").value;') == 'shared-draft-across-tasks'
            assert not any(f.latest_user(r) == 'browser-queued-cancel' for r in h.provider.captured())
            select_task(stop_id)
            js('document.getElementById("interrupt").click();return true;')
            selected('browser-stop', 'cancelled')
            assert js('return document.getElementById("instruction").value;') == 'shared-draft-across-tasks'
            assert js('return document.getElementById("submit-steer").disabled && document.getElementById("interrupt").disabled;')
            stop.set()
            assert js('return document.getElementById("task-list").children.length;') == 7
            assert js('return !window.fixtureRequests.some(([method,path])=>method==="GET" && path.startsWith("/tasks/"));')
            assert js('return document.getElementById("poll-error").textContent;') == ''
            assert js('return document.getElementById("history-error").textContent;') == ''
            assert js('return document.getElementById("action-error").textContent;') == ''
            print('PASS: real Firefox observe history, shared queue/steer input, selected-task guards, safe text/stderr, queued cancel/running stop, refresh and draft retention, no detail UI or per-task GET')
            command('Marionette:Quit', {'flags': ['eForceQuit']})
            browser.wait(10)
            browser = None
            h.shutdown()
    finally:
        if sock:
            sock.close()
        if browser and browser.poll() is None:
            os.killpg(browser.pid, signal.SIGTERM)
            try:
                browser.wait(8)
            except subprocess.TimeoutExpired:
                os.killpg(browser.pid, signal.SIGKILL)
                browser.wait()
        h.doCleanups()
if __name__ == '__main__':
    main()
