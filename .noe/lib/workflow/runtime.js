// Standalone NOE execution boundary; tests may inject agent operations and commands.
import {execFile} from "node:child_process";
import {appendFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, writeFileSync} from "node:fs";
import {isAbsolute, join, relative, resolve} from "node:path";
import process from "node:process";
import {WorkflowError, repositoryPath, validateCommand} from "./protocol.js";

/**
 * @typedef {Pick<typeof import("noe:agent"), "spawn_agent" | "queue_agent" | "wait_task" | "stop_agent">} AgentOperations
 * @param {{cwd?: string, agents?: AgentOperations, execute?: typeof execFile}} options
 */
export async function createRuntime({cwd = process.cwd(), agents, execute = execFile} = {}) {
  const root = realpathSync(cwd);
  // Agent cwd is inherited from NOE, not configurable through spawn_agent.
  if (!agents && root !== realpathSync(process.cwd())) {
    throw new WorkflowError("cwd", "Launch standalone NOE from the target repository root");
  }
  const operations = agents ?? await import("noe:agent");
  return {
    cwd: root,
    agents: operations,
    exists(path) { return existsSync(resolve(root, path)); },
    read(path) { return readFileSync(resolve(root, path), "utf8"); },
    async command(command) {
      validateCommand(command);
      try {
        const output = await execute(command.argv[0], command.argv.slice(1), {cwd: resolve(root, command.cwd)});
        return {exitCode: 0, stdout: output.stdout, stderr: output.stderr};
      } catch (error) {
        // Nonzero child exit is data. Start errors, cancellation and shutdown propagate.
        if (typeof error.code !== "number" || !Number.isInteger(error.code)) throw error;
        return {exitCode: error.code, stdout: error.stdout ?? "", stderr: error.stderr ?? ""};
      }
    },
  };
}
export async function git(runtime, args) {
  const result = await runtime.command({argv: ["git", ...args], cwd: ".", purpose: "Workflow Git operation"});
  if (result.exitCode !== 0) throw new WorkflowError("git", "git " + args[0] + " failed: " + result.stderr);
  return result.stdout;
}
function nulPaths(text) { return text.split("\0").filter(Boolean); }
function within(root, path) {
  const rel = relative(root, path);
  return rel === "" || (!rel.startsWith("../") && rel !== ".." && !isAbsolute(rel));
}
export async function prepareRepository(runtime, config) {
  const root = realpathSync((await git(runtime, ["rev-parse", "--show-toplevel"])).trim());
  if (root !== runtime.cwd) throw new WorkflowError("cwd", "NOE cwd must be the target Git root, not a subdirectory");
  const branchResult = await runtime.command({argv: ["git", "symbolic-ref", "--quiet", "--short", "HEAD"], cwd: ".", purpose: "Reject detached HEAD"});
  if (branchResult.exitCode !== 0) throw new WorkflowError("git-preflight", "Detached HEAD is not supported");
  const branch = branchResult.stdout.trim();
  if (branch !== config.git.branch) throw new WorkflowError("git-preflight", "Current branch does not match configured branch");
  await git(runtime, ["check-ref-format", "refs/heads/" + config.git.pushBranch]);
  const pushUrls = (await git(runtime, ["remote", "get-url", "--push", "--all", config.git.remote])).trim().split("\n");
  if (pushUrls.length !== 1 || !pushUrls[0]) throw new WorkflowError("git-preflight", "One known push destination is required");
  const tracked = nulPaths(await git(runtime, ["diff", "--name-only", "-z", "--"]));
  const index = nulPaths(await git(runtime, ["diff", "--cached", "--name-only", "-z", "--"]));
  if (tracked.length || index.length) throw new WorkflowError("git-preflight", "Preexisting tracked/index edits must be resolved before running");
  const head = (await git(runtime, ["rev-parse", "--verify", "HEAD"])).trim();
  const untracked = nulPaths(await git(runtime, ["ls-files", "--others", "-z", "--"]));
  const metadata = resolve(root, (await git(runtime, ["rev-parse", "--absolute-git-dir"])).trim());
  const artifactBase = resolve(root, config.artifactsDir ?? (await git(runtime, ["rev-parse", "--git-path", "noe-workflow"])).trim());
  // Never put diagnostic files in implementation paths, even with a custom override.
  if (within(root, artifactBase) && !within(metadata, artifactBase)) {
    throw new WorkflowError("artifacts", "Artifacts must be outside the worktree or inside its Git metadata");
  }
  mkdirSync(artifactBase, {recursive: true});
  const actualBase = realpathSync(artifactBase);
  if (within(root, actualBase) && !within(metadata, actualBase)) {
    throw new WorkflowError("artifacts", "Artifact directory resolves into the worktree");
  }
  return {root, branch, head, untracked, metadata, artifactBase: actualBase};
}
export function createArtifactStore(base) {
  const directory = mkdtempSync(join(base, "run-"));
  let sequence = 0;
  return {
    directory,
    json(name, value) {
      repositoryPath(name, "artifact name");
      writeFileSync(join(directory, name), JSON.stringify(value, null, 2) + "\n");
    },
    event(type, detail = {}) {
      appendFileSync(join(directory, "events.jsonl"), JSON.stringify({sequence: ++sequence, at: new Date().toISOString(), type, ...detail}) + "\n");
    },
    async command(runtime, command, label) {
      const result = await runtime.command(command);
      const prefix = "command-" + (++sequence);
      writeFileSync(join(directory, prefix + ".stdout"), result.stdout);
      writeFileSync(join(directory, prefix + ".stderr"), result.stderr);
      this.event("command", {label, command, exitCode: result.exitCode, stdout: prefix + ".stdout", stderr: prefix + ".stderr"});
      return result;
    },
  };
}
export const WORKER_BOUNDARY = [
  "You are a trusted workflow worker, not the orchestrator. No delegation.",
  "Discover and follow applicable AGENTS.md and repository work rules, including nested files for every path you inspect, plan or change. Missing root AGENTS.md does not mean nested rules are absent.",
  "Never commit, stage, push, tag, rewrite Git history, release, or change Git configuration.",
  "Never change requirements or weaken acceptance behavior, hide test failures, or waive a failed command.",
  "Only the orchestrator owns Git mutations. Report blockers or concrete escalation evidence in the required JSON.",
].join("\n");

export function createAgentPool(runtime, store) {
  const workers = new Map();
  let taskSequence = 0;
  return {
    async task(slot, model, instruction, system = "") {
      const taskNumber = ++taskSequence;
      const prefix = "task-" + taskNumber;
      store.json(prefix + "-request.json", {slot, model, instruction, system});
      let worker = workers.get(slot);
      let receipt;
      if (!worker) {
        receipt = await runtime.agents.spawn_agent(
          {...model, name: "workflow-" + slot, system: WORKER_BOUNDARY + "\n" + system},
          instruction,
          {keepAlive: true, allowDelegation: false},
        );
        worker = {agentId: receipt.agentId};
        workers.set(slot, worker);
      } else {
        const queued = await runtime.agents.queue_agent({agentId: worker.agentId, instruction});
        receipt = {agentId: worker.agentId, commandId: queued.commandId};
      }
      store.event("task-accepted", {taskNumber, slot, ...receipt});
      if (!receipt.commandId) throw new WorkflowError("agent-protocol", "Accepted task has no command ID");
      // Intentional completion-only wait: standalone controller, no polling or steer continuation.
      const outcome = await runtime.agents.wait_task({agentId: worker.agentId, commandId: receipt.commandId, includeAnswer: true});
      store.json(prefix + "-outcome.json", outcome);
      store.event("task-finished", {taskNumber, slot, state: outcome.state});
      if (outcome.state !== "completed") {
        throw new WorkflowError(outcome.state === "cancelled" ? "cancelled" : "agent-task-failed", outcome.error ?? ("Task ended in state " + outcome.state));
      }
      if (typeof outcome.answer !== "string") throw new WorkflowError("agent-protocol", "Completed task has no answer");
      return outcome.answer;
    },
    async close() {
      const errors = [];
      // stop_agent acknowledges shutdown; owner lifetime ultimately reclaims descendants.
      await Promise.all([...workers.values()].map(async worker => {
        try { await runtime.agents.stop_agent({agentId: worker.agentId}); }
        catch (error) { errors.push(String(error)); }
      }));
      workers.clear();
      store.event("cleanup", {errors});
      if (errors.length) throw new WorkflowError("cleanup", errors.join("; "));
    },
  };
}
