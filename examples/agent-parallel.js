import { createAgent } from "noe:agent";

const left = process.argv[2] ?? "examples/data/agent/left.json";
const right = process.argv[3] ?? "examples/data/agent/right.json";
const parent = createAgent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "parent",
  system: "子agentの実行結果を使って集計してください。",
});

const result = await parent.run(
  `run_js内でcreateAgent({name:"left"})とcreateAgent({name:"right"})を作り、` +
  `Promise.allで並行実行してください。左の子には ${JSON.stringify(left)}、` +
  `右の子には ${JSON.stringify(right)} を実際に読んで数値配列の合計を計算させてください。` +
  `各子もrun_jsを使い、親は子の回答を統合してください。` +
  `最後はleft、right、totalを持つJSONで回答してください。`,
  { cacheKey: "noe-parallel-v1" },
);
console.log(result);
