#!/usr/bin/env python3
"""Optional real-browser test. Requires Firefox (Marionette); no WebDriver package.
Run: python3 examples/agent-server/test_browser.py
NOE_BIN and FIREFOX_BIN override the executables. Uses the local SSE fixture.
"""
import json, socket, subprocess, tempfile, os, signal, pathlib, sys
sys.dont_write_bytecode = True
import test_server as f

def main():
    h = f.AgentServerTests('test_without_events_still_runs_and_retains_results')
    h.setUp()
    browser = None
    sock = None
    try:
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
                return command('WebDriver:ExecuteScript', {'script': script, 'args': [], 'newSandbox': False, 'sandbox': 'default'})['value']

            def wait(script, desc):
                return f.eventually(lambda: js(script), desc, timeout=20)
            command('WebDriver:NewSession', {'capabilities': {'alwaysMatch': {'acceptInsecureCerts': False}}})
            command('WebDriver:Navigate', {'url': h.startup['url']})
            wait('return document.getElementById("agent-status").textContent.includes("idle")', 'initial poll')
            js('document.getElementById("instruction").value="browser-first";document.getElementById("task-form").requestSubmit();return true;')
            wait('return document.getElementById("answer").textContent==="answer:browser-first"', 'first rendered answer')
            bad = '<img src=x onerror="window.fixtureXSS=true">'
            js('document.getElementById("instruction").value=' + json.dumps(bad) + ';document.getElementById("task-form").requestSubmit();return true;')
            wait('return document.getElementById("answer").textContent.includes("<img")', 'safe answer')
            assert js('return !window.fixtureXSS && document.getElementById("answer").children.length===0;')
            gate = h.provider.gate()
            started = __import__('threading').Event()

            def reply(r):
                if f.latest_user(r) == 'browser-hold':
                    started.set()
                    h.provider.wait(gate)
                return {'content': 'answer:' + f.latest_user(r)}
            h.provider.reply = reply
            js('document.getElementById("instruction").value="browser-hold";document.getElementById("task-form").requestSubmit();return true;')
            assert started.wait(10)
            wait('return !document.getElementById("submit-steer").disabled', 'running steer controls')
            js('document.getElementById("steer-instruction").value="browser-correction";document.getElementById("steer-form").requestSubmit();return true;')
            wait('return document.getElementById("steers").textContent.includes("accepted")', 'accepted steer display')
            gate.set()
            wait('return document.getElementById("answer").textContent==="answer:browser-correction" && document.getElementById("steers").textContent.includes("applied")', 'applied steer and answer')
            stop = h.provider.gate()
            stopping = __import__('threading').Event()

            def reply2(r):
                if f.latest_user(r) == 'browser-stop':
                    stopping.set()
                    h.provider.wait(stop)
                return {'content': 'answer:' + f.latest_user(r)}
            h.provider.reply = reply2
            js('document.getElementById("instruction").value="browser-stop";document.getElementById("task-form").requestSubmit();return true;')
            assert stopping.wait(10)
            wait('return !document.getElementById("interrupt").disabled && document.getElementById("task-meta").textContent.includes("running")', 'running interrupt control')
            js('document.getElementById("instruction").value="draft-must-survive-poll";document.getElementById("interrupt").click();return true;')
            wait('return document.getElementById("task-meta").textContent.includes("cancelled")', 'cancelled task display')
            assert js('return document.getElementById("instruction").value==="draft-must-survive-poll";')
            stop.set()
            assert js('return document.getElementById("task-list").children.length;') == 4
            assert js('return document.getElementById("poll-error").textContent;') == ''
            print('PASS: real Firefox submit/list/answer, inert HTML, steer accepted->applied, interrupt, draft retention')
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
