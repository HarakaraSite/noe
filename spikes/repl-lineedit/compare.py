#!/usr/bin/env python3
"""Linux PTY comparison. Requires pyte for assertions on the rendered screen."""
import argparse
import fcntl
import http.server
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import tempfile
import termios
import threading
import time

import pyte
from wcwidth import wcwidth

ROOT = Path(__file__).resolve().parents[2]
TIMEOUT = 5


def terminal_cells(text, columns):
    """A wide glyph cannot start in the terminal's final single column."""
    cells = 0
    for char in text:
        width = wcwidth(char)
        if width == 2 and cells % columns == columns - 1:
            cells += 1
        cells += width
    return cells


class Probe:
    def __init__(self, binary, editor, directory, width=80, flags=(), separate_output=False):
        self.master, self.slave = pty.openpty()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, width, 0, 0))
        self.original = termios.tcgetattr(self.slave)
        self.screen = pyte.Screen(width, 24)
        self.stream = pyte.ByteStream(self.screen)
        self.stdout = bytearray()
        self.stderr = bytearray()
        self.trace = directory / "trace.jsonl"
        env = dict(os.environ, TERM="xterm-256color", LANG="C.UTF-8")

        def controlling_terminal():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)

        self.process = subprocess.Popen(
            [str(binary), "--editor", editor, "--trace", str(self.trace), *flags],
            stdin=self.slave, stdout=subprocess.PIPE if separate_output else self.slave,
            stderr=subprocess.PIPE, cwd=ROOT, env=env, preexec_fn=controlling_terminal,
        )
        self.fds = {self.master: (self.stdout, True), self.process.stderr.fileno(): (self.stderr, False)}
        if separate_output:
            self.fds[self.process.stdout.fileno()] = (self.stdout, False)
        self.separate_output = separate_output

    def pump(self, timeout=0.02):
        ready, _, _ = select.select(list(self.fds), [], [], timeout)
        for fd in ready:
            data = os.read(fd, 65536)
            target, render = self.fds[fd]
            if not data:
                del self.fds[fd]
                continue
            target.extend(data)
            if render:
                self.stream.feed(data)

    def wait(self, predicate, label):
        deadline = time.monotonic() + TIMEOUT
        while time.monotonic() < deadline:
            self.pump()
            if predicate():
                return
            if self.process.poll() is not None:
                break
        raise AssertionError(f"{label}; exit={self.process.poll()}, out={bytes(self.stdout)!r}, err={bytes(self.stderr)!r}")

    def editing(self):
        self.wait(lambda: not (termios.tcgetattr(self.slave)[3] & termios.ICANON), "editing mode")

    def send(self, data):
        if isinstance(data, str):
            data = data.encode()
        os.write(self.master, data)

    def events(self):
        if not self.trace.exists():
            return []
        lines = self.trace.read_text().splitlines()
        # An event may be in the middle of its write.
        result = []
        for line in lines:
            try:
                result.append(json.loads(line))
            except json.JSONDecodeError:
                pass
        return result

    def evaluate(self, keys, expected_input, expected_output, display=True, prompt="> ", error=None, cursor=None):
        count = len(self.events()) + 1
        self.editing()
        if display:
            self.wait(lambda: self.screen.cursor.x == len(prompt) and self.screen.display[self.screen.cursor.y].rstrip() == prompt.rstrip(), "empty prompt")
        prompt_row = self.screen.cursor.y
        self.send(keys)
        if display:
            self.wait(lambda: prompt + expected_input in "".join(row.rstrip() for row in self.screen.display), "rendered edited input")
            cells = terminal_cells(prompt + expected_input[:len(expected_input) if cursor is None else cursor], self.screen.columns)
            row, column = divmod(cells, self.screen.columns)
            expected = {(prompt_row + row, column)}
            if cells and column == 0:
                expected.add((prompt_row + row - 1, self.screen.columns))  # pending VT auto-wrap
            self.wait(lambda: (self.screen.cursor.y, self.screen.cursor.x) in expected, "rendered cursor position")
        self.send(b"\r")
        self.wait(lambda: len(self.events()) >= count, "evaluation trace")
        event = self.events()[count - 1]
        assert event["Input"] == expected_input, event
        assert event["Output"] == expected_output, event
        if error:
            assert error in event["Error"], event
        else:
            assert not event["Error"], event
        return event

    def finish(self, code):
        self.process.wait(timeout=TIMEOUT)
        for _ in range(3):
            self.pump(0)
        assert self.process.returncode == code, (self.process.returncode, bytes(self.stderr))
        assert termios.tcgetattr(self.slave) == self.original, "terminal settings were not restored"

    def close(self):
        if self.process.poll() is None:
            os.killpg(self.process.pid, signal.SIGKILL)
            self.process.wait()
        self.process.stderr.close()
        if self.process.stdout is not None:
            self.process.stdout.close()
        os.close(self.master)
        os.close(self.slave)


def edited_line(p, name):
    cases = {
        "left-insert": ("1+3\x1b[D2+", "1+2+3", "6"),
        "right-backspace": ("1+23\x1b[D\x1b[D\x1b[C\x7f", "1+3", "4"),
        "home-end-delete": ("'abXc'\x1b[H\x1b[C\x1b[C\x1b[C\x1b[3~\x1b[F", "'abc'", "'abc'"),
        "ctrl-a-e": ("1+3\x012+\x05+4", "2+1+3+4", "10"),
        "word-move": ("11 + 22\x1bb3 + \x05", "11 + 3 + 22", "36"),
        "word-delete": ("11 + 22\x173", "11 + 3", "14"),
        "kill-end": ("1+9999\x01\x1b[C\x1b[C\x0b2", "1+2", "3"),
        "kill-start": ("123\x154+5", "4+5", "9"),
        "japanese-delete": ("'あXいう'\x1b[D\x1b[D\x1b[D\x1b[D\x1b[3~\x05", "'あいう'", "'あいう'"),
        "japanese-backspace": ("'あいう'\x1b[D\x1b[D\x7f\x05", "'あう'", "'あう'"),
        "wrapped-ascii": ("'" + "a" * 75 + "'\x1b[Db\x05", "'" + "a" * 75 + "b'", "'" + "a" * 75 + "b'"),
        # Align full-width glyphs to a two-cell boundary. pyte 0.8.2 does
        # not model a wide glyph starting in the final single column.
        "wrapped-japanese": ("' " + "あ" * 30 + "'\x1b[Dい\x05", "' " + "あ" * 30 + "い'", "' " + "あ" * 30 + "い'"),
    }
    p.evaluate(*cases[name], cursor={"left-insert": 4, "right-backspace": 2}.get(name))
    p.editing()
    p.send(b"\x04")
    p.finish(0)


def scenario(p, name):
    if name in EDIT_CASES:
        return edited_line(p, name)
    if name == "history":
        p.evaluate("let count=1", "let count=1", "")
        p.evaluate("count+=1", "count+=1", "2")
        p.evaluate("\x1b[A", "count+=1", "3")
        p.evaluate("\x1b[A\x7f2\x05", "count+=2", "5")
    elif name == "history-search":
        p.evaluate("let sample=17", "let sample=17", "")
        p.evaluate("1+2", "1+2", "3")
        p.evaluate("\x12sample\x1b[F", "let sample=17", "", display=False, error="already")
    elif name == "recovery":
        p.evaluate("let count=1", "let count=1", "")
        p.evaluate("throw new Error('bad')", "throw new Error('bad')", "", error="bad")
        p.evaluate("count+2", "count+2", "3")
        p.wait(lambda: b"bad" in p.stderr, "stderr diagnostic")
        assert b"Error: bad" not in p.stdout
    elif name == "hooks":
        p.evaluate("1+1", "1+1", "2")
        p.evaluate("conso\t.log('HOOK')", "console.log('HOOK')", "", prompt="... ")
        p.wait(lambda: b"\r\nHOOK\r\n" in p.stdout, "console output after completion")
    elif name == "exit-7":
        p.evaluate("noe.exit(7)", "noe.exit(7)", "")
        return p.finish(7)
    elif name == "input-failure":
        return p.finish(1)
    elif name in ("ctrl-c-idle", "sigint-idle"):
        p.editing()
        if name.startswith("sigint"):
            os.kill(p.process.pid, signal.SIGINT)
        else:
            p.send(b"\x03")
        return p.finish(130)
    elif name == "ctrl-c-search":
        p.evaluate("let sample=17", "let sample=17", "")
        p.editing()
        start = len(p.stdout)
        p.send("\x12sample")
        p.wait(lambda: b"sample" in p.stdout[start:], "history search started")
        p.send(b"\x03")
        return p.finish(130)
    elif name == "ctrl-c-completion":
        p.editing()
        start = len(p.stdout)
        p.send("con\t\t")
        p.wait(lambda: b"console" in p.stdout[start:] and b"constant" in p.stdout[start:], "completion candidates")
        p.send(b"\x03")
        return p.finish(130)
    elif name in ("ctrl-c-eval", "sigint-eval", "ctrl-c-queued"):
        p.editing()
        p.send("console.log('RUNNING');while(true){}\r")
        p.wait(lambda: b"\r\nRUNNING\r\n" in p.stdout, "JS evaluation started")
        assert termios.tcgetattr(p.slave) == p.original, "evaluation is not in cooked mode"
        if name.endswith("queued"):
            p.send("1+2\r")
        if name.startswith("sigint"):
            os.kill(p.process.pid, signal.SIGINT)
        else:
            p.send(b"\x03")
        return p.finish(130)
    elif name == "ctrl-c-http":
        ready, disconnected = threading.Event(), threading.Event()

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                self.connection.settimeout(TIMEOUT)
                ready.set()
                if self.connection.recv(1) == b"":
                    disconnected.set()

            def log_message(self, *args):
                pass

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            p.editing()
            p.send(f"await fetch('http://127.0.0.1:{server.server_port}')\r")
            assert ready.wait(TIMEOUT), "HTTP request did not start"
            assert termios.tcgetattr(p.slave) == p.original, "HTTP wait is not in cooked mode"
            p.send(b"\x03")
            p.finish(130)
            assert disconnected.wait(TIMEOUT), "HTTP connection was not cancelled"
        finally:
            server.shutdown()
            server.server_close()
            thread.join()
        return
    elif name == "redirected-output":
        p.wait(lambda: b"> " in p.stdout, "plain prompt")
        assert termios.tcgetattr(p.slave) == p.original
        p.send("1+2\r")
        p.wait(lambda: len(p.events()) == 1, "plain evaluation")
        assert p.events()[0]["Output"] == "3"
        p.send(b"\x04")
        p.finish(0)
        assert b"\x1b" not in p.stdout
        return
    else:
        raise AssertionError(name)
    p.editing()
    p.send(b"\x04")
    p.finish(0)


EDIT_CASES = ["left-insert", "right-backspace", "home-end-delete", "ctrl-a-e", "word-move", "word-delete", "kill-end", "kill-start", "japanese-delete", "japanese-backspace", "wrapped-ascii", "wrapped-japanese"]
CASES = EDIT_CASES + ["history", "history-search", "recovery", "hooks", "exit-7", "input-failure", "ctrl-c-idle", "sigint-idle", "ctrl-c-search", "ctrl-c-completion", "ctrl-c-eval", "sigint-eval", "ctrl-c-queued", "ctrl-c-http", "redirected-output"]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--case", action="append")
    args = parser.parse_args()
    results = []
    for editor in ("readline", "liner"):
        for name in args.case or CASES:
            with tempfile.TemporaryDirectory(prefix="noe-lineedit-") as directory:
                flags = ["--hooks"] if name in ("hooks", "ctrl-c-completion") else ["--fail-input"] if name == "input-failure" else []
                probe = Probe(args.binary.resolve(), editor, Path(directory), width=40 if name.startswith("wrapped") else 80, flags=flags, separate_output=name == "redirected-output")
                try:
                    scenario(probe, name)
                    result = {"editor": editor, "case": name, "status": "pass", "exit_code": probe.process.returncode, "terminal_restored": termios.tcgetattr(probe.slave) == probe.original, "events": probe.events()}
                except Exception as exc:
                    result = {"editor": editor, "case": name, "status": "fail", "detail": str(exc)}
                finally:
                    probe.close()
                results.append(result)
                print(editor, name, result["status"], result.get("detail", ""), flush=True)
        pipe = subprocess.run([str(args.binary.resolve()), "--editor", editor], input=b"let count=1\ncount+=1\nthrow new Error('bad')\ncount+2", capture_output=True, cwd=ROOT, timeout=TIMEOUT)
        passed = pipe.returncode == 0 and pipe.stdout == b"> > 2\n> > 4\n" and b"bad" in pipe.stderr and b"\x1b" not in pipe.stdout
        result = {"editor": editor, "case": "pipe-final-line", "status": "pass" if passed else "fail"}
        if not passed:
            result["detail"] = repr((pipe.returncode, pipe.stdout, pipe.stderr))
        results.append(result)
        print(editor, "pipe-final-line", result["status"], result.get("detail", ""), flush=True)
    args.output.write_text(json.dumps(results, ensure_ascii=False, indent=2) + "\n")
    raise SystemExit(any(row["status"] == "fail" for row in results))


if __name__ == "__main__":
    main()
