# Outer-agent instruction template

Use this when an existing agent receives a user request to implement settled requirements through the standalone workflow. Keep the requesting agent, actual workflow parent, and workers distinct.

## Requester / outer agent

1. Confirm the user wants implementation now and the requirements are settled. Identify the actual target repository root, the copied workflow run.js path, the requirements path, and any optional config. Do not invent requirements or launch the workflow from the NOE source checkout when another repository is the target.
2. Explain that ordinary execution requires configured agent-provider access (ChatGPT by default), with no mandatory model-based approval gates or separate decision-provider credential requirement. The optional `.noe/workflow.json` supports role overrides only; the experimental `decision` setting and its defaults have been removed without compatibility support. `--help` is offline and does not require credentials.
3. Start one awaited standalone NOE subprocess from the actual target root. For example:

~~~js
// Await the workflow from the target repository root.
import {execFile} from "node:child_process";
const args = [
  "run",
  "/absolute/path/to/target/.noe/lib/workflow/run.js",
  "--requirements",
  "docs/settled-requirements.md"
];
// Optional: args.push("--config", ".noe/workflow.json");
const result = await execFile("noe", args, {cwd: "/absolute/path/to/target"});
console.log(result.stdout);
~~~

Keep the owning command/tool alive until it completes. Do not detach it or import the launcher into a managed run_js call as though it were a resumable workflow API. If the subprocess fails, reports native failure/cancellation, or ends without completing, report that fact rather than treating an agent ending as successful delivery.
4. The workflow parent owns the plan, reviews, implementation coordination, validation judgments, Git commits and final push decision. Do not become a second coordinator, duplicate the parent's reviews, or independently repeat the same task stages. The requester may relay concise user-facing progress or a blocker but should let the parent make its judgments.
5. Return the parent's natural-language answer to the user with its achieved behavior, evidence, local commits, actual push state, and remaining issues. If push failed or did not occur, describe delivery as incomplete.

## Actual workflow parent

The parent is the native root agent started by the launcher, not the requester and not a worker. It follows workflow.md, reads the unchanged requirements and repository work rules/current Git state, owns all completion judgments and Git operations, and may delegate within the assigned task. Its launch explicitly permits delegation.

## Workers

The workflow parent may start scoped implementation, exploration, validation, and independent review agents as useful. Workers share files but are not isolated. They follow their task instructions, do not silently change requirements, commit/push/release, recursively launch the workflow, or further-delegate by default. Implementation workers implement and run the planned focused checks in one continuous loop, including related repairs, without a parent approval pause before testing. They report actual evidence and concrete blockers to the parent, who inspects the candidate before independent review and owns slice completion. A worker is never a second workflow parent.

## Direct CLI use

A user may run run.js directly from the target repository root; that starts the same workflow parent. There is no --repo option, in-process resume, or custom Git/artifact controller.
