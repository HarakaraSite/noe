import { readFile, writeFile, appendFile } from "node:fs/promises";
import { run_agent, spawn_agent, queue_agent, wait_task, stop_agent } from "noe:agent";
import { spawn } from "noe:process";
import { decide } from "noe:decision";

// Task-loop experiment using wait_task instead of polling. Run with cwd set to this directory.
const cwd = process.cwd();
const requirements = await readFile(`${cwd}/requirements.txt`, "utf8");
const base = { provider: "openai-chatgpt", model: "gpt-6.1-sol", effort: "high" };
const report = { configuration: {agent: base, gate: {provider: "openrouter", model: "typesafe/jev-1.14"}}, startedAt: new Date().toISOString(), steps: [], polls: 0, waits: 0 };
const reviewerSystem = "あなたはレビュー担当です。渡された要件と対象だけを評価し、ファイルを変更しないでください。具体的な要件違反があれば根拠と修正内容を自然な日本語で簡潔に述べ、なければ要件を満たすと述べてください。任意の好みや対象外の改善提案は除外してください。JSON形式や定型の判定ラベルは不要です。";

async function record(stage, details) {
  const entry = { stage, at: new Date().toISOString(), ...details };
  report.steps.push(entry);
  await appendFile(`${cwd}/steps.jsonl`, JSON.stringify(entry) + "\n");
  console.log(stage, JSON.stringify(details));
}

function completed(snapshot) {
  const result = snapshot.lastResult;
  if (result?.state !== "completed") {
    throw new Error(`task did not complete: ${JSON.stringify(result ?? snapshot)}`);
  }
  return result;
}

async function review(stage, target) {
  const result = completed(await run_agent({ ...base, name: stage, system: reviewerSystem },
    JSON.stringify({ requirements, target })));
  await record(`${stage}.answer`, result);
  // The review stays free-form text. Only the decision model produces a branch value.
  const decision = await decide({
    provider: "openrouter", model: "typesafe/jev-1.14",
    state: { requirements, target, review: result.answer },
    questions: {
      next: {
        type: "choice",
        instructions: "要件、対象、レビューを照合し、この工程を次へ進めてよいか判断してください。レビューは判断材料であり、対象にない問題や単なる好みを採用しないでください。具体的な未解決の要件違反があればrevise、なければproceedを選んでください。",
        criteria: {
          proceed: "対象は明示要件を満たしており、修正が必要な具体的な要件違反は残っていない。次の工程へ進む。",
          revise: "対象に具体的な要件違反または未解決のテスト失敗があり、この工程の修正が必要。",
        },
      },
    },
  });
  await record(`${stage}.decision`, decision);
  const choice = decision.answers?.next?.choice;
  if (choice !== "proceed" && choice !== "revise") {
    throw new Error(`decision did not return a recognized choice: ${JSON.stringify(decision)}`);
  }
  return { choice, review: result.answer };
}

async function submit(workerId, instruction, stage) {
  const receipt = await queue_agent({ agentId: workerId, instruction });
  await record(`${stage}.accepted`, receipt);
  report.waits++;
  const result = await wait_task({ agentId: workerId, commandId: receipt.commandId });
  if (result.state !== "completed") {
    throw new Error(`task did not complete: ${JSON.stringify(result)}`);
  }
  await record(`${stage}.completed`, result);
  return result.answer;
}

async function test(stage) {
  const child = await spawn(process.argv[0], ["run", `${cwd}/test.js`], { cwd, stdin: "ignore" });
  let output;
  let errors;
  let status;
  try {
    [output, errors, status] = await Promise.all([
      new Response(child.stdout).text(), new Response(child.stderr).text(), child.exited,
    ]);
  } finally {
    await child.close();
  }
  const value = { ...status, output, errors };
  await record(stage, value);
  return value;
}

let worker;
try {
  await test("baseline.test");
  worker = await spawn_agent({ ...base, name: "workflow-worker",
    system: `指定された小さな作業を自律的に完了してください。作業場所は${cwd}です。指示で指定されたファイルだけを扱ってください。` },
    undefined, { keepAlive: true });
  await record("worker.started", worker);

  let plan = await readFile(`${cwd}/plan.md`, "utf8");
  let planRevisions = 0;
  while (true) {
    const evaluation = await review("plan-review", plan);
    if (evaluation.choice === "proceed") break;
    const answer = await submit(worker.agentId, JSON.stringify({
      task: "レビューのうち要件に即した指摘を反映して計画を修正してください。実装はまだ行わず、修正後の計画全文を自然な日本語で回答してください。JSON形式は不要です。",
      requirements, plan, review: evaluation.review,
    }), "plan-revision");
    plan = answer;
    await writeFile(`${cwd}/plan.md`, plan + "\n");
    planRevisions++;
  }

  await submit(worker.agentId, JSON.stringify({
    task: `レビューを通過した計画を実装してください。run_jsで${cwd}/solution.jsを書き換え、export function sumPositiveIntegers(values)を実装してください。test.jsなど他のファイルは変更しないでください。テストは呼出し元のJSが行います。`,
    requirements, plan,
  }), "implementation");

  let codeRevisions = 0;
  while (true) {
    const tests = await test("implementation.test");
    const code = await readFile(`${cwd}/solution.js`, "utf8");
    const evaluation = await review("code-review", { code, tests });
    if (tests.code === 0 && evaluation.choice === "proceed") break;
    await submit(worker.agentId, JSON.stringify({
      task: `テスト失敗と採用するレビュー指摘を修正し、${cwd}/solution.jsだけを書き換えてください。`,
      requirements, review: evaluation.review, tests,
    }), "code-revision");
    codeRevisions++;
  }
  report.outcome = "completed";
  report.planRevisions = planRevisions;
  report.codeRevisions = codeRevisions;
} catch (error) {
  report.outcome = "failed";
  report.error = String(error?.stack ?? error);
  throw error;
} finally {
  if (worker) await stop_agent({ agentId: worker.agentId });
  report.finishedAt = new Date().toISOString();
  await writeFile(`${cwd}/report.json`, JSON.stringify(report, null, 2) + "\n");
  console.log("WORKFLOW_RESULT", JSON.stringify({ outcome: report.outcome, polls: report.polls, waits: report.waits,
    planRevisions: report.planRevisions, codeRevisions: report.codeRevisions, error: report.error }));
}
