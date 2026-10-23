import { createHash, randomUUID } from "node:crypto";
import { createReadStream, existsSync } from "node:fs";
import { appendFile, mkdir, readFile, readdir, rename, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { open } from "noe:fs-handle";

const INDEX_WIDTH = 17; // Sixteen decimal digits and a newline per byte offset.
const encoder = new TextEncoder();
const sessionPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const offsetLine = value => String(value).padStart(INDEX_WIDTH - 1, "0") + "\n";
const resultKey = commandId => createHash("sha256").update(commandId).digest("hex");

async function readRange(path, offset, length) {
  const file = await open(path);
  try {
    const { data, bytesRead } = await file.readAt(offset, length);
    if (bytesRead !== length) throw new Error("History index or data is incomplete");
    return new TextDecoder().decode(data);
  } finally {
    await file.close();
  }
}

export class HistoryQueryError extends Error {}

export class SessionStore {
  constructor(root, metadata) {
    this.root = resolve(root);
    this.sessionId = metadata.sessionId;
    this.current = { metadata, count: 0, bytes: 0 };
    this.loaded = new Map([[this.sessionId, Promise.resolve(this.current)]]);
    this.writing = Promise.resolve();
  }

  static async create(root, details) {
    const metadata = { ...details, sessionId: randomUUID(), startedAt: new Date().toISOString() };
    const store = new SessionStore(root, metadata);
    const directory = store.directory(metadata.sessionId);
    // Publish only a fully initialized session. Interrupted staging directories
    // do not match sessionPattern and remain available for manual inspection.
    const staging = join(store.root, ".initializing-" + metadata.sessionId);
    await mkdir(join(staging, "results"), { recursive: true });
    await Promise.all([
      writeFile(join(staging, "metadata.json"), JSON.stringify(metadata) + "\n"),
      writeFile(join(staging, "history.jsonl"), ""),
      writeFile(join(staging, "offsets.idx"), offsetLine(0)),
    ]);
    await rename(staging, directory);
    return store;
  }

  directory(sessionId) {
    if (typeof sessionId !== "string" || !sessionPattern.test(sessionId)) {
      throw new HistoryQueryError("Invalid session ID");
    }
    return join(this.root, sessionId);
  }

  resultPath(sessionId, commandId) {
    return join(this.directory(sessionId), "results", resultKey(commandId));
  }

  hasResult(sessionId, commandId) {
    return existsSync(this.resultPath(sessionId, commandId));
  }

  append(payload) {
    const next = this.writing.then(async () => {
      const record = { ...payload, seq: this.current.count + 1, time: new Date().toISOString() };
      const line = JSON.stringify(record) + "\n";
      const nextOffset = this.current.bytes + encoder.encode(line).byteLength;
      if (!Number.isSafeInteger(nextOffset)) throw new Error("History byte offset exceeds safe integer range");
      const directory = this.directory(this.sessionId);
      await appendFile(join(directory, "history.jsonl"), line);
      await appendFile(join(directory, "offsets.idx"), offsetLine(nextOffset));
      this.current.count = record.seq;
      this.current.bytes = nextOffset;
      if (record.type === "result") {
        await writeFile(this.resultPath(this.sessionId, record.commandId), String(record.seq));
      }
      return record;
    });
    // Keep a failed write visible to callers; do not silently resume with a corrupt index.
    this.writing = next.then(() => {});
    this.writing.catch(() => {}); // The append caller receives the original error.
    return next;
  }

  async sessions() {
    const names = (await readdir(this.root)).filter(name => sessionPattern.test(name));
    const sessions = await Promise.all(names.map(async sessionId => {
      try {
        const metadata = JSON.parse(await readFile(join(this.directory(sessionId), "metadata.json"), "utf8"));
        if (!metadata || metadata.sessionId !== sessionId || typeof metadata.startedAt !== "string") {
          throw new Error("Invalid session metadata");
        }
        return { ...metadata, current: sessionId === this.sessionId };
      } catch (error) {
        // Preserve visibility of older incomplete sessions without preventing
        // healthy sessions from being listed. Never delete or rewrite their data.
        return { sessionId, name: "読み込み不可", startedAt: "", current: false, error: error.message ?? String(error) };
      }
    }));
    sessions.sort((a, b) => b.startedAt.localeCompare(a.startedAt));
    return { currentSessionId: this.sessionId, sessions };
  }

  async load(sessionId) {
    this.directory(sessionId);
    let loading = this.loaded.get(sessionId);
    if (!loading) {
      loading = this.rebuild(sessionId);
      this.loaded.set(sessionId, loading);
    }
    return loading;
  }

  async rebuild(sessionId) {
    const directory = this.directory(sessionId);
    const metadata = JSON.parse(await readFile(join(directory, "metadata.json"), "utf8"));
    await mkdir(join(directory, "results"), { recursive: true });
    const temporary = join(directory, "offsets.rebuild");
    await writeFile(temporary, offsetLine(0));
    const decoder = new TextDecoder();
    let pending = "";
    let offsets = "";
    let count = 0;
    let bytes = 0;
    for await (const chunk of createReadStream(join(directory, "history.jsonl"))) {
      pending += decoder.decode(chunk, { stream: true });
      let end;
      while ((end = pending.indexOf("\n")) !== -1) {
        const line = pending.slice(0, end);
        pending = pending.slice(end + 1);
        const record = JSON.parse(line);
        if (record.seq !== count + 1) throw new Error("History record sequence is invalid");
        bytes += encoder.encode(line + "\n").byteLength;
        count++;
        offsets += offsetLine(bytes);
        if (record.type === "result") {
          await writeFile(this.resultPath(sessionId, record.commandId), String(record.seq));
        }
        if (count % 256 === 0) {
          await appendFile(temporary, offsets);
          offsets = "";
        }
      }
    }
    // A process may end during an append. Only complete JSONL records are indexed;
    // never rewrite the original history or invent a terminal task result.
    if (offsets) await appendFile(temporary, offsets);
    await rename(temporary, join(directory, "offsets.idx"));
    return { metadata, count, bytes };
  }

  /** @param {{sessionId?: string, before?: number, after?: number, limit?: number}} query */
  async history({ sessionId = this.sessionId, before, after, limit = 100 } = {}) {
    if (!Number.isSafeInteger(limit) || limit < 1 || limit > 1000 ||
        (before !== undefined && (!Number.isSafeInteger(before) || before < 1)) ||
        (after !== undefined && (!Number.isSafeInteger(after) || after < 0)) ||
        (before !== undefined && after !== undefined)) {
      throw new HistoryQueryError("Use limit 1..1000 and either an exclusive before or after cursor");
    }
    if (sessionId === this.sessionId) await this.writing;
    const session = await this.load(sessionId);
    const total = session.count;
    let start;
    let end;
    if (after !== undefined) {
      start = Math.min(after, total) + 1;
      end = Math.min(total, start + limit - 1);
    } else {
      end = before === undefined ? total : Math.min(total, before - 1);
      start = Math.max(1, end - limit + 1);
    }
    if (start > end) return { sessionId, records: [], firstSeq: 0, lastSeq: 0, total };
    const directory = this.directory(sessionId);
    const offsets = (await readRange(join(directory, "offsets.idx"), (start - 1) * INDEX_WIDTH,
      (end - start + 2) * INDEX_WIDTH)).trim().split("\n").map(Number);
    const text = await readRange(join(directory, "history.jsonl"), offsets[0], offsets[offsets.length - 1] - offsets[0]);
    const records = text.trimEnd().split("\n").map(line => JSON.parse(line));
    if (sessionId !== this.sessionId) {
      for (const record of records) {
        if (record.type === "operation" && record.operation === "queue" && record.receipt && !record.error) {
          record.resultRecorded = this.hasResult(sessionId, record.commandId);
        }
      }
    }
    return { sessionId, records, firstSeq: start, lastSeq: end, total };
  }
}
