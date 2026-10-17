import { exercise } from "mcp/exercise";

const result = await exercise(JSON.parse(process.argv[2]));
if (result.largeInputBytes !== 270000) throw new Error("UTF-8 input length mismatch");
console.log(JSON.stringify({ route: "main", ...result }));
