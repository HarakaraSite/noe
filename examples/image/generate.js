import { generate_image } from "noe:image";
import { dirname, resolve } from "node:path";
import { mkdir } from "node:fs/promises";

const [
  provider = "openrouter",
  model = "openai/gpt-image-2.5-flare",
  outputArgument = "tmp/noe-image.png",
] = process.argv.slice(2);

const output = resolve(outputArgument);
await mkdir(dirname(output), { recursive: true });

const result = await generate_image({
  provider,
  model,
  prompt: "A clean flat illustration of a small red sailboat on a calm blue sea, centered, no text.",
  output,
});

// Print metadata only; image bytes stay in the host.
console.log(JSON.stringify(result, null, 2));
