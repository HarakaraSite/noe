import { createAgent } from "noe:agent";

const file = process.argv[2] ?? "README.md";
const agent = createAgent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "summary",
  system: "日本語で簡潔に回答してください。",
});

const result = await agent.run(
  `作業ディレクトリの ${JSON.stringify(file)} を実際に読み、要点を3点にまとめてください。`,
  { cacheKey: "noe-summary-v1" },
);
console.log(result);
