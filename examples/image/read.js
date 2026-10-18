import { run_agent } from "noe:agent";
import { resolve } from "node:path";

const [inputPath, provider = "openai-chatgpt", model = "gpt-6-luna"] = process.argv.slice(2);
if (!inputPath) {
  throw new Error("Usage: noe run examples/image/read.js <image-path> [provider] [model]");
}

const imagePath = resolve(inputPath);
const instruction = [
  "Describe the visible contents of this image in Japanese:",
  `Image path: ${JSON.stringify(imagePath)}`,
  "Use run_js to import read_image from noe:image and call await read_image(" + JSON.stringify(imagePath) + ").",
  "Do not console.log the read_image result; a successful call attaches the image automatically.",
  "Start the run_js source with a short English // comment.",
  "Answer briefly and mention any visible text.",
].join("\n");

const result = await run_agent({
  provider,
  model,
  effort: "auto",
  name: "image-reader",
}, instruction);

if (result.lastResult?.state !== "completed") {
  throw new Error(result.lastResult?.error ?? `agent ended with ${result.reason}`);
}
console.log(result.lastResult.answer);
