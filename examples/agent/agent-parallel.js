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
  `observe_agent({agentId})を使って両方の状態を確認してください。live観察はすぐ戻るため、` +
  `各子のstateがstoppedまたはfailedになるまで短く待って再観察してください。` +
  `各snapshot.lastResult.answerを使って結果をまとめ、最後はleft、right、totalを持つJSONで回答してください。`);

if (result.lastResult?.state !== "completed") {
  throw new Error(result.lastResult?.error ?? `agent ended with ${result.reason}`);
}
console.log(result.lastResult.answer);
