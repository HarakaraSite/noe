import { spawn } from "noe:process";

// NoeTransport implements the SDK Transport contract with newline-delimited
// JSON over a child process's standard streams. The caller owns process
// selection and supplies all command arguments explicitly.
export class NoeTransport {
  constructor({ command, args, cwd }) {
    if (typeof command !== "string" || !command) {
      throw new TypeError("command must be a nonempty string");
    }
    if (!Array.isArray(args) || args.some(value => typeof value !== "string")) {
      throw new TypeError("args must be an array of strings");
    }
    if (typeof cwd !== "string" || !cwd) {
      throw new TypeError("cwd must be a nonempty string");
    }
    this.command = command;
    this.args = [...args];
    this.cwd = cwd;
    this.onclose = undefined;
    this.onerror = undefined;
    this.onmessage = undefined;
    this.child = undefined;
    this.exitStatus = undefined;
    this.closed = false;
    this.closing = false;
    this.started = false;
    this.startSettled = undefined;
    this.resolveStart = undefined;
    this.closePromise = undefined;
    this.cleanupPromise = undefined;
  }

  async start() {
    if (this.started || this.closed) throw new Error("transport can only be started once");
    this.started = true;
    this.startSettled = new Promise(resolve => { this.resolveStart = resolve; });
    try {
      this.child = await spawn(this.command, this.args, {
        cwd: this.cwd,
        stdin: "pipe",
        stdout: "pipe",
        stderr: "inherit",
      });
      if (this.closing || this.closed) {
        const child = this.child;
        await child.close();
        try { this.exitStatus = await child.exited; } catch (_) {}
        this.child = undefined;
        this.finish();
        return;
      }
      if (!this.child.stdin || !this.child.stdout) {
        throw new Error("spawn did not provide piped stdin and stdout");
      }
      this.writer = this.child.stdin.getWriter();
      this.reader = this.child.stdout.getReader();
      this.reading = this.readMessages().catch(error => this.fail(error));
    } catch (error) {
      await this.fail(error);
      throw error;
    } finally {
      this.resolveStart?.();
      this.resolveStart = undefined;
    }
  }

  async readMessages() {
    const decoder = new TextDecoder();
    let pending = "";
    for (;;) {
      const { value, done } = await this.reader.read();
      if (done) break;
      pending += decoder.decode(value, { stream: true });
      let newline;
      while ((newline = pending.indexOf("\n")) >= 0) {
        const line = pending.slice(0, newline);
        pending = pending.slice(newline + 1);
        if (!line.trim()) continue;
        this.onmessage?.(JSON.parse(line));
      }
    }
    pending += decoder.decode();
    if (pending.trim()) throw new Error("incomplete MCP message at EOF");

    if (!this.closing) throw new Error("unexpected stdout EOF");
    this.exitStatus = await this.child.exited;
  }

  async send(message) {
    if (!this.writer || this.closed || this.closing) {
      throw new Error("transport is not open");
    }
    try {
      await this.writer.write(new TextEncoder().encode(JSON.stringify(message) + "\n"));
    } catch (error) {
      void this.fail(error);
      throw error;
    }
  }

  close() {
    if (this.closePromise) return this.closePromise;
    if (this.cleanupPromise) return this.cleanupPromise;
    if (!this.child && this.started && this.startSettled) {
      this.closing = true;
      this.closePromise = this.closeAfterStart();
      return this.closePromise;
    }
    this.closePromise = this.closeChildGracefully();
    return this.closePromise;
  }

  async closeAfterStart() {
    await this.startSettled;
    if (this.cleanupPromise) return this.cleanupPromise;
    if (this.child) return this.closeChildGracefully();
    this.finish();
  }

  async closeChildGracefully() {
    if (!this.child) {
      this.finish();
      return;
    }
    const child = this.child;
    this.closing = true;
    try {
      // Give an EOF-aware server a short chance to exit cleanly. child.close()
      // then owns force cleanup if the process ignores stdin EOF. Cover the
      // stdin close itself too, since a full pipe can keep writer.close() held.
      let timer;
      const deadline = new Promise(resolve => {
        timer = setTimeout(() => resolve(undefined), 1000);
      });
      let status;
      try {
        status = await Promise.race([(async () => {
          await this.writer.close();
          return await child.exited;
        })(), deadline]);
      } finally {
        clearTimeout(timer);
      }
      if (!status) await child.close();
      status = await child.exited;
      this.exitStatus = status;
      await child.close();
      try {
        await this.reading;
      } catch (error) {
        if (!this.closed) throw error;
      }
      this.child = undefined;
      this.finish();
    } catch (error) {
      await this.fail(error);
      throw error;
    }
  }

  async fail(error) {
    if (this.closed) return this.cleanupPromise;
    this.closed = true;
    this.closing = true;
    try { this.onerror?.(error); } catch (_) {}
    try { this.onclose?.(); } catch (_) {}
    this.cleanupPromise = (async () => {
      const child = this.child;
      if (!child) return;
      // Start process closure before aborting the writer: WritableStream.abort
      // may wait for an in-flight native write, which child.close() must cancel.
      const closeProcess = child.close().catch(() => {});
      const abortWriter = Promise.resolve(this.writer?.abort(error)).catch(() => {});
      await Promise.all([closeProcess, abortWriter]);
      try { this.exitStatus = await child.exited; } catch (_) {}
      this.child = undefined;
    })();
    await this.cleanupPromise;
    return this.cleanupPromise;
  }

  finish() {
    if (this.closed) return;
    this.closed = true;
    this.onclose?.();
  }
}
