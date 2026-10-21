#!/usr/bin/env python3
"""Local integration tests: python3 examples/agent-server/test_server.py -v.

Requires a built ./noe, not Node or provider credentials. All model responses
come from a loopback OpenRouter-compatible SSE fixture. Each test owns its
configuration, runtime registry, event log, work directory, and processes.
"""

import concurrent.futures
import http.client
import http.server
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import threading
import time
import traceback
import unittest
from urllib.parse import urlsplit


ROOT = Path(__file__).resolve().parents[2]
NOE = Path(os.environ.get("NOE_BIN", str(ROOT / "noe"))).resolve()
SERVER = Path(__file__).with_name("server.js")
TIMEOUT = 15
TERMINAL = {"completed", "failed", "cancelled"}


def eventually(predicate, description, timeout=TIMEOUT):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        last = predicate()
        if last:
            return last
        time.sleep(0.025)
    raise AssertionError("timed out waiting for %s (last=%r)" % (description, last))


def latest_user(request):
    return next(m["content"] for m in reversed(request["messages"])
                if m["role"] == "user")


def transcript(request):
    return [(m["role"], m.get("content")) for m in request["messages"]
            if m["role"] != "system"]


def tool(code, call_id="fixture-tool"):
    return {"tool_calls": [{"index": 0, "id": call_id, "type": "function",
                             "function": {"name": "run_js", "arguments":
                                          json.dumps({"code": code})}}]}


class Provider:
    """Thread-safe request recorder; gates are always released during cleanup."""

    def __init__(self):
        self.requests = []
        self.errors = []
        self.lock = threading.Lock()
        self.gates = []
        self.reply = lambda request: {"content": "answer:" + latest_user(request)}
        owner = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                try:
                    assert self.path == "/chat/completions", self.path
                    assert self.headers.get("Authorization") == "Bearer local-fixture-key"
                    request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                    assert request["model"] == "fixture/model", request
                    assert request["stream"] is True
                    assert request.get("reasoning", {}).get("effort") in (None, "auto")
                    assert [t["function"]["name"] for t in request["tools"]] == ["run_js"]
                    messages = request["messages"]
                    assert messages[-1]["role"] == "user"
                    assert messages[-1]["content"].startswith("NOE_CONTEXT_STATUS\n")
                    assert any(m["role"] == "user" and
                               not str(m.get("content", "")).startswith("NOE_CONTEXT_STATUS\n")
                               for m in messages[:-1])
                    assert not any(m["role"] == "system" and
                                   str(m.get("content", "")).startswith("NOE_CONTEXT_STATUS")
                                   for m in request["messages"])
                    # The runtime status remains a user message in the model
                    # history. Hide diagnostic messages from fixture behavior
                    # so replies and transcript assertions track user tasks.
                    request["messages"] = [m for m in messages[:-1]
                                           if not (m["role"] == "user" and
                                                   str(m.get("content", "")).startswith("NOE_CONTEXT_STATUS\n"))]
                    with owner.lock:
                        owner.requests.append(request)
                    reply = owner.reply(request)
                    if "status" in reply:
                        body = json.dumps({"error": {"message": "intentional fixture HTTP 400"}}).encode()
                        self.send_response(reply["status"])
                        self.send_header("Content-Type", "application/json")
                        self.send_header("Content-Length", str(len(body)))
                        self.end_headers()
                        self.wfile.write(body)
                        return
                    self.send_response(200)
                    self.send_header("Content-Type", "text/event-stream")
                    self.end_headers()
                    deltas = [{"role": "assistant", "content": None}]
                    deltas.extend(reply.get("deltas", []))
                    if "content" in reply:
                        deltas.append({"content": reply["content"]})
                    for delta in deltas:
                        self.chunk(delta, None)
                    self.chunk({}, reply.get("finish", "stop"))
                    self.wfile.write(b'data: [DONE]\n\n')
                    self.wfile.flush()
                except (BrokenPipeError, ConnectionResetError):
                    pass  # Expected when the owner interrupts an in-flight call.
                except Exception:
                    with owner.lock:
                        owner.errors.append(traceback.format_exc())
                    self.send_error(400)

            def chunk(self, delta, finish):
                chunk = {"id": "fixture", "object": "chat.completion.chunk", "created": 1,
                         "model": "fixture/model", "choices": [
                             {"index": 0, "delta": delta, "finish_reason": finish}]}
                self.wfile.write(("data: " + json.dumps(chunk) + "\n\n").encode())

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = "http://127.0.0.1:%d" % self.server.server_port

    def gate(self):
        gate = threading.Event()
        self.gates.append(gate)
        return gate

    def wait(self, gate):
        if not gate.wait(TIMEOUT):
            raise AssertionError("fixture gate was not released")

    def captured(self):
        with self.lock:
            return list(self.requests)

    def close(self):
        for gate in self.gates:
            gate.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(2)


class AgentServerTests(unittest.TestCase):
    def setUp(self):
        if not NOE.is_file() or not SERVER.is_file():
            self.fail("build ./noe and provide examples/agent-server/server.js first")
        self.temp = tempfile.TemporaryDirectory(prefix="noe-agent-server-test-")
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)
        self.provider = Provider()
        self.addCleanup(self.provider.close)
        self.proc = None
        self.addCleanup(self.cleanup_processes)
        self.addCleanup(self.report_failure)
        self.runtime = self.work / "runtime"
        self.runtime.mkdir(mode=0o700)
        self.events = self.work / "events.jsonl"
        self.stdout = self.work / "stdout"
        self.stderr = self.work / "stderr"
        self.env = {k: v for k, v in os.environ.items()
                    if not k.startswith("NOE_") and k not in ("OPENROUTER_API_KEY",)}
        key = self.work / "key"
        key.write_text("local-fixture-key\n")
        key.chmod(0o600)
        config = self.work / "config.json"
        config.write_text(json.dumps({"providers": {"openrouter": {
            "baseURL": self.provider.url, "keyFile": str(key)}}}))
        self.env.update({"NOE_CONFIG_FILE": str(config),
                         "NOE_OPENROUTER_BASE_URL": self.provider.url,
                         "NOE_OPENROUTER_API_KEY_FILE": str(key),
                         "NOE_AGENT_RUNTIME_DIR": str(self.runtime),
                         "HOME": str(self.work), "XDG_CONFIG_HOME": str(self.work / "config")})

    def report_failure(self):
        # Keep successful runs quiet; include real CLI diagnostics on assertion failures.
        result = getattr(self._outcome, "result", None)
        if result and any(test is self for test, _ in result.errors + result.failures):
            for path in (self.stdout, self.stderr):
                if path.exists():
                    print("\n--- %s ---\n%s" % (path.name, path.read_text()[-12000:]), file=sys.stderr)
            if self.provider.errors:
                print("\n".join(self.provider.errors), file=sys.stderr)

    def start(self, events=True):
        args = [str(NOE), "run"]
        if events:
            args.append("--events=" + str(self.events))
        args.extend([str(SERVER), "--port", "0", "--provider", "openrouter",
                     "--model", "fixture/model", "--effort", "auto"])
        if events:
            args.extend(["--events-path", str(self.events)])
        with self.stdout.open("wb") as out, self.stderr.open("wb") as err:
            self.proc = subprocess.Popen(args, cwd=self.work, env=self.env,
                                         stdin=subprocess.DEVNULL, stdout=out, stderr=err,
                                         start_new_session=True)

        def startup():
            if self.proc.poll() is not None:
                self.fail("server exited during startup: " + self.stderr.read_text())
            for line in self.stdout.read_text().splitlines():
                try:
                    value = json.loads(line)
                except ValueError:
                    continue
                if isinstance(value, dict) and value.get("url"):
                    return value
            return None

        self.startup = eventually(startup, "JSON startup line")
        self.assertTrue(self.startup.get("agentId"), self.startup)
        parsed = urlsplit(self.startup["url"])
        self.assertIn(parsed.hostname, ("127.0.0.1", "localhost", "::1"))
        self.address = (parsed.hostname, parsed.port)
        self.assertTrue(parsed.port)
        self.assertEqual(self.provider.captured(), [], "idle agent contacted provider")
        eventually(lambda: bool(list(self.runtime.iterdir())), "independent runtime registration")

    def request(self, method, path, value=None, raw=None, expected=200, headers=None):
        conn = http.client.HTTPConnection(*self.address, timeout=3)
        body = raw if raw is not None else (json.dumps(value) if value is not None else None)
        try:
            conn.request(method, path, body=body,
                         headers={"Content-Type": "application/json", **(headers or {})})
            response = conn.getresponse()
            data = response.read().decode()
            expected = (expected,) if isinstance(expected, int) else expected
            self.assertIn(response.status, expected, (method, path, response.status, data))
            try:
                return json.loads(data)
            except ValueError:
                self.assertNotEqual(response.status, 202, data)
                return data
        finally:
            conn.close()

    def submit(self, instruction):
        task = self.request("POST", "/tasks", {"instruction": instruction}, expected=202)
        self.assertTrue(task.get("id"), task)
        self.assertIn(task["state"], {"queued", "starting", "running"})
        return task

    def get(self, task):
        return self.request("GET", "/tasks/" + task["id"])

    def finished(self, task, state="completed", answer=None):
        result = eventually(lambda: (value if value["state"] in TERMINAL else None)
                            if (value := self.get(task)) else None,
                            "task " + task["id"])
        self.assertEqual(result["state"], state, result)
        if state != "cancelled":
            self.assertTrue(result.get("runId"), result)
        if answer is not None:
            self.assertEqual(result.get("answer"), answer, result)
            self.assertIsInstance(result.get("result"), dict, result)
            self.assertEqual(result["result"].get("answer"), answer, result)
            self.assertEqual(result["result"].get("state"), state, result)
        return result

    def wait_file(self, name):
        path = self.work / name
        return eventually(lambda: path.read_text().strip() if path.exists() else None, name)

    def event_records(self):
        if not self.events.exists():
            return []
        records = []
        for line in self.events.read_text().splitlines():
            try:
                records.append(json.loads(line))
            except ValueError:
                pass  # Writer may still be appending the last line.
        return records

    def clean_registry(self):
        eventually(lambda: not list(self.runtime.iterdir()), "empty runtime registry")
        listing = subprocess.run([str(NOE), "agents"], cwd=self.work, env=self.env,
                                 capture_output=True, text=True, timeout=5)
        self.assertEqual(listing.returncode, 0, listing.stderr)
        self.assertEqual(listing.stdout.strip(), "No active agents.")

    @staticmethod
    def pid_gone(pid):
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return True
        return False

    def owned_pids_gone(self):
        # run_js executions share this NOE process. Only the explicit OS
        # subprocess has a PID that should disappear when its owner ends.
        pids = [int(self.wait_file("spawned.pid"))]
        for pid in pids:
            eventually(lambda pid=pid: self.pid_gone(pid), "PID %d reaped" % pid)

    def cleanup_processes(self):
        if self.proc is not None and self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(5)
            except subprocess.TimeoutExpired:
                os.killpg(self.proc.pid, signal.SIGKILL)
                self.proc.wait(5)
        # Fallback only: assertions above must prove normal ownership cleanup.
        for path in self.work.glob("*.pid"):
            try:
                os.kill(int(path.read_text()), signal.SIGKILL)
            except (ProcessLookupError, ValueError):
                pass

    def shutdown(self):
        self.request("POST", "/shutdown", expected=202)
        self.assertEqual(self.proc.wait(TIMEOUT), 0, self.stderr.read_text())
        self.clean_registry()
        self.assertEqual(self.provider.errors, [])

    def test_startup_validation_and_bind_failure_leave_no_agents(self):
        for args in (["--port", "-1"], ["--port", "65536"], ["--port", "1.5"],
                     ["--effort", "invalid"], ["--model", ""], ["--unknown", "x"],
                     ["--port", "0", "--port", "1"], ["--provider"]):
            result = subprocess.run([str(NOE), "run", str(SERVER)] + args,
                                    cwd=self.work, env=self.env, capture_output=True,
                                    text=True, timeout=5)
            self.assertEqual(result.returncode, 2, (args, result.stderr))
            self.assertIn("Usage:", result.stderr)
        help_result = subprocess.run([str(NOE), "run", str(SERVER), "--help"],
                                     cwd=self.work, env=self.env, capture_output=True,
                                     text=True, timeout=5)
        self.assertEqual(help_result.returncode, 0, help_result.stderr)
        # The fixture already owns this port: startup must reclaim its idle agent.
        failed = subprocess.run([str(NOE), "run", str(SERVER), "--port",
                                 str(self.provider.server.server_port), "--provider",
                                 "openrouter", "--model", "fixture/model", "--effort", "auto"],
                                cwd=self.work, env=self.env, capture_output=True,
                                text=True, timeout=5)
        self.assertEqual(failed.returncode, 1, failed.stderr)
        self.clean_registry()
        self.assertEqual(self.provider.captured(), [])

    def test_fast_tasks_retain_every_result_and_full_conversation(self):
        self.start()
        instructions = ["fast-%02d" % i for i in range(12)]
        with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
            tasks = list(pool.map(self.submit, instructions))
        self.assertEqual(len({t["id"] for t in tasks}), len(tasks))
        results = [self.finished(t, answer="answer:" + text)
                   for t, text in zip(tasks, instructions)]
        self.assertEqual(len({t["runId"] for t in results}), len(tasks))
        captured = self.provider.captured()
        self.assertEqual(len(captured), len(tasks))
        history = []
        for request in captured:
            text = latest_user(request)
            self.assertIn(text, instructions)
            self.assertEqual(transcript(request), history + [("user", text)])
            history.extend([("user", text), ("assistant", "answer:" + text)])
        listed = self.request("GET", "/tasks")
        self.assertEqual(listed["agent"]["agentId"], self.startup["agentId"])
        self.assertEqual({t["id"] for t in listed["tasks"]}, {t["id"] for t in tasks})
        self.assertTrue(all(t["state"] == "completed" for t in listed["tasks"]))
        for task, result in zip(tasks, results):
            self.assertEqual(self.get(task)["answer"], result["answer"])
        self.shutdown()

    def test_provider_failure_then_continuation(self):
        self.provider.reply = lambda r: ({"status": 400} if latest_user(r) == "fail-now"
                                         else {"content": "answer:" + latest_user(r)})
        self.start()
        self.finished(self.submit("before"), answer="answer:before")
        failed = self.submit("fail-now")
        after = self.submit("after-failure")
        result = self.finished(failed, "failed")
        self.assertTrue(result.get("error") or result.get("result"), result)
        self.finished(after, answer="answer:after-failure")
        captured = self.provider.captured()
        self.assertEqual(len(captured), 3, "HTTP 400 must not be retried")
        self.assertEqual(transcript(captured[-1]), [
            ("user", "before"), ("assistant", "answer:before"),
            ("user", "fail-now"), ("user", "after-failure")])
        self.assertEqual(self.get(failed)["state"], "failed")
        self.shutdown()

    def test_validation_pending_cancel_and_wrong_states(self):
        gate = self.provider.gate()
        def reply(request):
            if latest_user(request) == "hold":
                self.provider.wait(gate)
            return {"content": "answer:" + latest_user(request)}
        self.provider.reply = reply
        self.start()
        self.assertIsInstance(self.request("GET", "/"), str)
        self.request("POST", "/tasks", {"instruction": "wrong media type"},
                     headers={"Content-Type": "text/plain"}, expected=415)
        for headers in ({"Host": "evil.invalid"}, {"Origin": "https://evil.invalid"},
                        {"Sec-Fetch-Site": "cross-site"}):
            self.request("POST", "/tasks", {"instruction": "must-not-create"},
                         headers=headers, expected=403)
        origin = "http://%s:%d" % self.address
        self.request("GET", "/tasks", headers={"Origin": origin})
        self.assertEqual(self.request("GET", "/tasks")["tasks"], [])
        for path in ("/unknown", "/tasks/missing"):
            self.request("GET", path, expected=404)
        for action in ("steer", "interrupt"):
            self.request("POST", "/tasks/missing/" + action,
                         {"instruction": "x"}, expected=404)
        for raw in ("{broken", "", "null", "[]", '"text"', "{}",
                    '{"instruction":""}', '{"instruction":"   "}',
                    '{"instruction":42}'):
            self.request("POST", "/tasks", raw=raw, expected=400)
        for method, path in (("PUT", "/tasks"), ("POST", "/tasks/missing"),
                             ("GET", "/shutdown")):
            self.request(method, path, expected=(404, 405))
        active = self.submit("hold")
        eventually(lambda: len(self.provider.captured()) == 1, "blocked provider call")
        pending = self.submit("never-execute")
        self.assertEqual(self.get(pending)["state"], "queued")
        self.request("POST", "/tasks/" + pending["id"] + "/steer",
                     {"instruction": "cannot steer pending"}, expected=409)
        self.request("POST", "/tasks/" + pending["id"] + "/interrupt", expected=202)
        self.finished(pending, "cancelled")
        self.request("POST", "/tasks/" + active["id"] + "/steer",
                     {"instruction": "wrong media type"},
                     headers={"Content-Type": "text/plain"}, expected=415)
        for raw in ("{bad", "{}", '{"instruction":" "}'):
            self.request("POST", "/tasks/" + active["id"] + "/steer", raw=raw, expected=400)
        gate.set()
        self.finished(active, answer="answer:hold")
        for action in ("steer", "interrupt"):
            self.request("POST", "/tasks/" + active["id"] + "/" + action,
                         {"instruction": "too late"}, expected=409)
        self.finished(self.submit("still-usable"), answer="answer:still-usable")
        self.assertEqual([latest_user(r) for r in self.provider.captured()], ["hold", "still-usable"])
        self.shutdown()

    def test_steer_during_tool_and_at_final_response_boundary(self):
        boundary = self.provider.gate()
        boundary_started = threading.Event()
        correction_started = threading.Event()
        correction_gate = self.provider.gate()
        final_correction_started = threading.Event()
        final_correction_gate = self.provider.gate()
        correction = "tool correction"
        final_correction = "boundary correction"
        code = ('// Wait for a deterministic test release marker.\n'
                'import {writeFileSync,existsSync} from "node:fs";\n'
                'import {setTimeout} from "node:timers/promises";\n'
                'writeFileSync("tool.ready","ready");\n'
                'while(!existsSync("tool.release")) await setTimeout(20);\n'
                'console.log("tool released");')
        def reply(request):
            text = latest_user(request)
            if text == "tool-steer":
                return {"deltas": [tool(code)], "finish": "tool_calls"}
            if text == correction:
                correction_started.set()
                self.provider.wait(correction_gate)
            if text == final_correction:
                final_correction_started.set()
                self.provider.wait(final_correction_gate)
            if text == "final-boundary":
                boundary_started.set()
                self.provider.wait(boundary)
                return {"content": "candidate answer"}
            return {"content": "answer:" + text}
        self.provider.reply = reply
        self.start()
        task = self.submit("tool-steer")
        self.wait_file("tool.ready")
        eventually(lambda: (self.get(task).get("progress") or {}).get("phase") == "run_js",
                   "HTTP progress identifies the blocked run_js")
        self.assertIn("Wait for a deterministic", self.get(task)["progress"].get("action", ""))
        receipt = self.request("POST", "/tasks/" + task["id"] + "/steer",
                               {"instruction": correction}, expected=202)
        self.assertTrue(receipt.get("commandId"), receipt)
        self.assert_pending_steer(task, receipt)
        self.assertNotEqual(self.get(task)["state"], "completed")
        (self.work / "tool.release").write_text("release")
        self.assertTrue(correction_started.wait(TIMEOUT))
        self.assert_steer_applied(task, receipt, correction)
        self.assertNotIn(self.get(task)["state"], TERMINAL)
        correction_gate.set()
        result = self.finished(task, answer="answer:" + correction)
        second = self.provider.captured()[1]
        self.assertIn(("user", correction), transcript(second))
        self.assertTrue(any(m["role"] == "tool" and "tool released" in m["content"]
                            for m in second["messages"]), second)
        task2 = self.submit("final-boundary")
        self.assertTrue(boundary_started.wait(TIMEOUT))
        receipt2 = self.request("POST", "/tasks/" + task2["id"] + "/steer",
                                {"instruction": final_correction}, expected=202)
        self.assert_pending_steer(task2, receipt2)
        boundary.set()
        self.assertTrue(final_correction_started.wait(TIMEOUT))
        self.assert_steer_applied(task2, receipt2, final_correction)
        self.assertNotIn(self.get(task2)["state"], TERMINAL)
        final_correction_gate.set()
        self.finished(task2, answer="answer:" + final_correction)
        self.assertEqual(transcript(self.provider.captured()[-1])[-3:], [
            ("user", "final-boundary"), ("assistant", "candidate answer"),
            ("user", final_correction)])
        self.assertEqual(self.get(task)["answer"], result["answer"])
        self.shutdown()

    def assert_pending_steer(self, task, receipt):
        self.assertEqual(receipt.get("status"), "accepted", receipt)
        steer = next(s for s in self.get(task)["steers"]
                     if s["commandId"] == receipt["commandId"])
        self.assertIn(steer["status"], {"accepted", "delivered"}, steer)
        self.assertNotEqual(steer.get("confirmation"), "event", steer)

    def assert_steer_applied(self, task, receipt, instruction):
        def applied():
            steers = self.get(task).get("steers", [])
            return next((s for s in steers if s.get("commandId") == receipt["commandId"]
                         and s.get("status") == "applied"
                         and s.get("confirmation") == "event"), None)
        steer = eventually(applied, "steer applied status from event log")
        self.assertEqual(steer["instruction"], instruction)
        eventually(lambda: any(e.get("type") == "command.applied" and
                               e.get("operation") == "steer" and
                               e.get("commandId") == receipt["commandId"]
                               for e in self.event_records()), "command.applied steer event")

    def install_owned_work(self):
        # Parent runtime -> child agent runtime -> spawned Python. Runtime
        # markers prove each in-process execution reached the owned work; only
        # the explicit subprocess has an independently reaped OS PID.
        parent_code = ('// Own a nested agent until this task is interrupted.\n'
                       'import {writeFileSync} from "node:fs";\n'
                       'writeFileSync("parent.started","yes");\n'
                       'await run_agent({name:"owned-test-child"},"owned-child");')
        child_script = ('import os,time; '
                        'open("spawned.pid","w").write(str(os.getpid())); time.sleep(120)')
        child_code = ('// Spawn an owned subprocess and wait for interruption.\n'
                      'import {writeFileSync} from "node:fs";\n'
                      'import {spawn} from "noe:process";\n'
                      'writeFileSync("child.started","yes");\n'
                      'const child=await spawn(' + json.dumps(sys.executable) + ',' +
                      json.dumps(["-c", child_script]) +
                      ',{stdin:"ignore",stdout:"ignore",stderr:"ignore"});\n'
                      'try { await child.exited; } finally { await child.close(); }')
        def reply(request):
            text = latest_user(request)
            if text in ("owned-parent", "owned-child"):
                code = parent_code if text == "owned-parent" else child_code
                return {"deltas": [tool(code)], "finish": "tool_calls"}
            return {"content": "answer:" + text}
        self.provider.reply = reply
        self.start()
        task = self.submit("owned-parent")
        for name in ("parent.started", "child.started", "spawned.pid"):
            self.wait_file(name)
        eventually(lambda: any(e.get("name") == "owned-test-child" and
                               e.get("type") == "agent.started" for e in self.event_records()),
                   "owned child agent started")
        return task

    @unittest.skipUnless(os.name == "posix", "POSIX process and signal assertions")
    def test_running_interrupt_cleans_owned_work_and_next_task_runs(self):
        task = self.install_owned_work()
        pending = self.submit("after-interrupt")
        receipt = self.request("POST", "/tasks/" + task["id"] + "/steer",
                               {"instruction": "cancel this pending steer"}, expected=202)
        self.assert_pending_steer(task, receipt)
        self.request("POST", "/tasks/" + task["id"] + "/interrupt", expected=202)
        self.finished(task, "cancelled")
        def cancelled_steer():
            return next((s for s in self.get(task)["steers"]
                         if s["commandId"] == receipt["commandId"]
                         and s["status"] == "cancelled"
                         and s.get("confirmation") == "event"), None)
        eventually(cancelled_steer, "cancelled steer confirmed by command.cancelled event")
        self.assertTrue(any(e.get("type") == "command.cancelled" and
                            e.get("operation") == "steer" and
                            e.get("commandId") == receipt["commandId"]
                            for e in self.event_records()))
        self.owned_pids_gone()
        self.finished(pending, answer="answer:after-interrupt")
        self.assertEqual(self.request("GET", "/tasks")["agent"]["agentId"], self.startup["agentId"])
        self.finished(self.submit("http-still-usable"), answer="answer:http-still-usable")
        self.assertEqual(self.get(task)["state"], "cancelled")
        self.shutdown()

    @unittest.skipUnless(os.name == "posix", "POSIX process and signal assertions")
    def test_graceful_shutdown_cleans_active_owned_work(self):
        self.install_owned_work()
        self.submit("must-not-run-on-shutdown")
        self.shutdown()
        self.owned_pids_gone()
        self.assertNotIn("must-not-run-on-shutdown", [latest_user(r) for r in self.provider.captured()])
        self.assertTrue(any(e.get("type") == "agent.cancelled" and
                            e.get("name") == "owned-test-child" for e in self.event_records()))

    @unittest.skipUnless(os.name == "posix", "POSIX process and signal assertions")
    def test_sigint_owner_cleans_active_owned_work(self):
        self.install_owned_work()
        self.proc.send_signal(signal.SIGINT)
        code = self.proc.wait(TIMEOUT)
        # The current CLI reports interrupted evaluation/event writing as exit 1;
        # 130 or signal termination is also conventional for owner SIGINT.
        self.assertIn(code, (1, 130, -signal.SIGINT), self.stderr.read_text())
        if code == 1:
            self.assertIn("context canceled", self.stderr.read_text())
        self.owned_pids_gone()
        self.clean_registry()
        self.assertEqual(self.provider.errors, [])

    def test_without_events_still_runs_and_retains_results(self):
        gate = self.provider.gate()
        started = threading.Event()
        def reply(request):
            if latest_user(request) == "no-events":
                started.set()
                self.provider.wait(gate)
                return {"content": "candidate without events"}
            return {"content": "answer:" + latest_user(request)}
        self.provider.reply = reply
        self.start(events=False)
        task = self.submit("no-events")
        self.assertTrue(started.wait(TIMEOUT))
        receipt = self.request("POST", "/tasks/" + task["id"] + "/steer",
                               {"instruction": "no-events correction"}, expected=202)
        self.assertIn(receipt.get("status"), {"accepted", "delivered", "applied", "unknown"})
        gate.set()
        result = self.finished(task, answer="answer:no-events correction")
        self.assertEqual(result["steers"][0]["commandId"], receipt["commandId"])
        self.assertEqual(result["steers"][0]["status"], "applied")
        self.assertFalse(self.events.exists())
        self.shutdown()


if __name__ == "__main__":
    unittest.main()
