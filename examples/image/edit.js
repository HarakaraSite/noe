// Edit actual source-image bytes. Authentication stays in the NOE host.
import { mkdirSync } from "noe:fs";
import { dirname } from "node:path";
import { edit_image } from "noe:image";

const [image, output, prompt = "Change the main subject to orange; keep its shape and composition."] = process.argv.slice(2);
if (!image || !output) throw new Error("Usage: noe run examples/image/edit.js INPUT OUTPUT [PROMPT]");
mkdirSync(dirname(output), { recursive: true });
const result = await edit_image({
  provider: "openai-codex-images",
  model: "gpt-image-2",
  images: [image],
  prompt,
  output,
});
console.log(JSON.stringify(result));
