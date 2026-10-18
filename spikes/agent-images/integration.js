import { writeFile } from "node:fs/promises";
import { run_agent } from "noe:agent";
import { generate_image } from "noe:image";

function answerMatches(answer, expected) {
  const text = String(answer || "");
  let parsed;
  try {
    const start = text.indexOf("{");
    const end = text.lastIndexOf("}");
    if (start >= 0 && end > start) parsed = JSON.parse(text.slice(start, end + 1));
  } catch (_) {}
  if (parsed && typeof parsed === "object") {
    return Object.keys(expected).every(key => String(parsed[key] || "").toLowerCase() === expected[key]);
  }
  return Object.entries(expected).every(([key, color]) =>
    new RegExp(key.replace(/[A-Z]/g, letter => `\\s*${letter.toLowerCase()}`) + `\\W{0,24}${color}`, "i").test(text));
}

function readbackMatches(answer) {
  const text = String(answer || "").toLowerCase();
  return /red circle/.test(text) && /white background/.test(text) &&
    !/(no|not|without)\s+(red circle|white background)/.test(text);
}

function safeFailure(error) {
  const result = { name: typeof error?.name === "string" ? error.name : "Error" };
  for (const field of ["code", "providerCode", "statusCode", "requestId"]) {
    if (error?.[field] !== undefined) result[field] = error[field];
  }
  return result;
}

async function saveReport(path, report) {
  await writeFile(path, JSON.stringify(report, null, 2) + "\n", { mode: 0o600 });
}

const [mode, provider, model, label, inputOrOutput, arg6, arg7] = process.argv.slice(2);
const expectedColors = mode === "vision" ? JSON.parse(arg6 || "null") : null;
const reportPath = mode === "vision" ? arg7 : arg6;
if (!mode || !provider || !model || !label || !inputOrOutput || !reportPath ||
    (mode === "vision" && (!expectedColors || typeof expectedColors !== "object" || Array.isArray(expectedColors)))) {
  throw new Error("usage: integration.js <vision> <provider> <model> <label> <input.png> <expected-colors-json> <report.json>; or <readback|generate|agent-generate-readback> <provider> <model> <label> <path> <report.json>");
}

const startedAt = Date.now();
const report = { mode, provider, model, label, durationMs: 0, success: false };

try {
  if (mode === "vision") {
    const path = inputOrOutput;
    const instruction = `Use run_js to import { read_image } from "noe:image" and call read_image(${JSON.stringify(path)}). A successful read_image attaches the actual image to your next model request. Do not inspect or decode the file with other JavaScript or filesystem APIs. Then identify the visible color in each quadrant. Answer only JSON with exactly these keys and a common English color name for each value: {"topLeft":"<color>","topRight":"<color>","bottomLeft":"<color>","bottomRight":"<color>"}.`;
    const snapshot = await run_agent({
      provider,
      model,
      effort: "low",
      name: label,
      maxSteps: 4,
      maxTokens: 4096,
      executionLimits: { providerCalls: 2 },
    }, instruction);
    const last = snapshot?.lastResult || {};
    report.state = last.state || snapshot?.state || "unknown";
    report.reason = snapshot?.reason || "";
    report.answer = typeof last.answer === "string" ? last.answer : "";
    report.expectedColors = expectedColors;
    report.imageMatch = answerMatches(report.answer, expectedColors);
    report.success = report.state === "completed" && report.imageMatch === true;
    if (!report.success) report.failure = { state: report.state, reason: report.reason };
  } else if (mode === "readback") {
    const path = inputOrOutput;
    const instruction = `Use run_js to import { read_image } from "noe:image" and call read_image(${JSON.stringify(path)}). A successful read_image attaches the actual image to your next model request. Do not inspect or decode the file with other JavaScript or filesystem APIs. Then report whether it contains one centered red circle on a plain white background, with no text. Answer briefly and mention any visible mismatch.`;
    const snapshot = await run_agent({
      provider,
      model,
      effort: "low",
      name: label,
      maxSteps: 4,
      maxTokens: 4096,
      executionLimits: { providerCalls: 2 },
    }, instruction);
    const last = snapshot?.lastResult || {};
    report.state = last.state || snapshot?.state || "unknown";
    report.reason = snapshot?.reason || "";
    report.answer = typeof last.answer === "string" ? last.answer : "";
    report.readbackMatch = readbackMatches(report.answer);
    report.success = report.state === "completed" && report.readbackMatch;
    if (!report.success) report.failure = { state: report.state, reason: report.reason };
  } else if (mode === "generate") {
    const result = await generate_image({
      provider,
      model,
      prompt: "A simple red circle centered on a plain white background",
      output: inputOrOutput,
      parameters: { quality: "low", size: "1024x1024" },
    });
    report.path = result.path;
    report.mimeType = result.mimeType;
    report.bytes = result.bytes;
    report.width = result.width;
    report.height = result.height;
    report.responseModel = result.responseModel || "";
    report.requestId = result.requestId || "";
    report.usage = result.usage || null;
    report.success = true;
  } else if (mode === "agent-generate-readback") {
    const output = inputOrOutput;
    const code = `const { generate_image, read_image } = await import("noe:image");\n` +
      `const generated = await generate_image({ provider: "openrouter", model: "openai/gpt-image-2.5-flare", prompt: "A simple red circle centered on a plain white background", output: ${JSON.stringify(output)}, parameters: { quality: "low", size: "1024x1024" } });\n` +
      `const image = await read_image(generated.path);\n` +
      `console.log(JSON.stringify({ path: generated.path, mimeType: generated.mimeType, bytes: generated.bytes, width: generated.width, height: generated.height, imageId: image.imageId }));`;
    const instruction = `In exactly one run_js call, execute this code:\n\n${code}\n\nThe successful read_image attaches the actual generated image to your next model request. Do not make another run_js call or inspect/decode file bytes. Base your conclusion on the visually attached image, not only on the generation prompt. After the tool returns, confirm whether the image contains one centered red circle on a plain white background with no text. If it does not, describe the mismatch.`;
    const snapshot = await run_agent({
      provider,
      model,
      effort: "low",
      name: label,
      maxSteps: 2,
      maxTokens: 4096,
      executionLimits: { providerCalls: 3 },
    }, instruction);
    const last = snapshot?.lastResult || {};
    report.state = last.state || snapshot?.state || "unknown";
    report.reason = snapshot?.reason || "";
    report.answer = typeof last.answer === "string" ? last.answer : "";
    report.outputPath = output;
    report.readbackMatch = readbackMatches(report.answer);
    report.success = report.state === "completed" && report.readbackMatch;
    if (!report.success) report.failure = { state: report.state, reason: report.reason };
  } else {
    throw new Error("mode must be vision, readback, or generate");
  }
} catch (error) {
  report.failure = safeFailure(error);
}

report.durationMs = Date.now() - startedAt;
await saveReport(reportPath, report);
