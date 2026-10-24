import { run_agent } from "noe:agent";

const left = process.argv[2] ?? "examples/agent/data/left.json";
const right = process.argv[3] ?? "examples/agent/data/right.json";
const result = await run_agent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "parent",
  system: "子agentの実行結果を使って集計してください。",
  cacheKey: "noe-parallel-v1",
  maxSteps: 8,
},
  `run_jsでspawn_agentを2回呼び、${JSON.stringify(left)}を読む子と${JSON.stringify(right)}を読む子を起動してください。` +
  `子はkeepAliveにせず、それぞれ数値配列の合計を計算します。spawn_agentは起動受領後に戻るので、` +
  `両方のagentId/commandIdを一つのwait_tasks({tasks:[...],includeAnswer:false})へ渡してください。` +
  `reasonがsteerなら結果をexportしてそのrun_jsを終了し、追加指示を読んで必要な対処を選んでください。` +
  `同じtool内で即座に待ち直したり、wait_taskをPromise.allで束ねたり、状態を定期ポーリングしたりしないでください。` +
  `完了したtaskの回答はwait_taskまたはread_task_resultで取得し、最後はleft、right、totalを持つJSONで回答してください。`);

if (result.lastResult?.state !== "completed") {
  throw new Error(result.lastResult?.error ?? `agent ended with ${result.reason}`);
}
console.log(result.lastResult.answer);
