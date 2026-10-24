// Aggregate request-local event metrics; never use execution.usage snapshots.
const token = (value) =>
  typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null;

const metric = (values, totalRequests) => {
  const known = values.filter((value) => value !== null);
  return {
    value: known.length ? known.reduce((sum, value) => sum + value, 0) : null,
    knownRequests: known.length,
    totalRequests,
  };
};

/** Analyze one complete JSONL file. Rates are fractions (0..1), not percentages. */
export function analyzeJSONL(text, file = "<input>", { below = 0.5 } = {}) {
  if (!Number.isFinite(below) || below < 0 || below > 1) {
    throw new Error("below must be a number from 0 to 1");
  }
  const requests = [];
  const overflows = [];
  for (const [index, line] of text.split("\n").entries()) {
    if (!line.trim()) continue;
    let event;
    try {
      event = JSON.parse(line);
    } catch {
      throw new Error(`${file}:${index + 1}: invalid JSON`);
    }
    if (event?.type === "model.finished") {
      const usage = event.usage;
      const input = token(usage?.promptTokens) ?? token(usage?.inputTokens);
      const cached = token(usage?.cachedTokens);
      const paired = input !== null && cached !== null && cached <= input;
      requests.push({
        line: index + 1,
        executionId: event.executionId ?? null,
        runId: event.runId ?? event.origin?.runId ?? null,
        modelStepId: event.modelStepId ?? event.origin?.modelStepId ?? null,
        requestId: event.requestId ?? null,
        model: event.model ?? null,
        inputTokens: input,
        cachedTokens: cached,
        uncachedTokens: paired ? input - cached : null,
        cacheHitRate: paired && input > 0 ? cached / input : null,
        rateStatus: !paired ? "unknown" : input === 0 ? "zero-input" : "known",
      });
    } else if (event?.type === "tool.finished") {
      // Only the model-facing result wrapper counts, not rawResult or capture text.
      const result = event.result;
      const available = event.modelResultAvailable !== false && Object.prototype.hasOwnProperty.call(event, "result");
      overflows.push(available ? (result?.outputLimit?.complete === false ? 1 : 0) : null);
    }
  }
  const paired = requests.filter((request) => request.uncachedTokens !== null);
  const pairedInput = paired.reduce((sum, request) => sum + request.inputTokens, 0);
  const pairedCached = paired.reduce((sum, request) => sum + request.cachedTokens, 0);
  return {
    file,
    modelCalls: requests.length,
    toolCalls: overflows.length,
    promptTokens: metric(requests.map((request) => request.inputTokens), requests.length),
    cachedTokens: metric(requests.map((request) => request.cachedTokens), requests.length),
    uncachedPromptTokens: metric(requests.map((request) => request.uncachedTokens), requests.length),
    cacheHitRate: {
      value: pairedInput > 0 ? pairedCached / pairedInput : null,
      knownRequests: paired.length,
      totalRequests: requests.length,
      inputTokens: paired.length ? pairedInput : null,
      cachedTokens: paired.length ? pairedCached : null,
    },
    outputOverflows: metric(overflows, overflows.length),
    below,
    // Unknown/zero-denominator rows remain visible rather than masquerading as hits.
    requests: requests
      .filter((request) => request.cacheHitRate === null || request.cacheHitRate < below)
      .sort((a, b) => (a.cacheHitRate ?? Infinity) - (b.cacheHitRate ?? Infinity) || a.line - b.line),
  };
}

const cell = (value) => String(value ?? "unknown")
  .replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;")
  .replaceAll("|", "&#124;").replaceAll("\r", " ").replaceAll("\n", " ");
const percent = (value) => value === null ? "unknown / n/a" : `${(100 * value).toFixed(2)}%`;
const total = (value, rate = false) =>
  `${rate ? percent(value.value) : value.value ?? "unknown"} (${value.knownRequests}/${value.totalRequests} requests)`;

/** Render several independent file summaries; no cross-file snapshot merging. */
export function toMarkdown(reports) {
  const lines = [
    "# NOE JSONL comparison",
    "",
    "Token sums and rates include only known values; coverage is shown per metric.",
    "Cache rate = sum(cached) / sum(input) on requests with both metrics known.",
    "",
    "| File | Model calls | Tool calls | Input tokens | Cached tokens | Non-cache tokens | Cache hit rate | Output overflows |",
    "| --- | ---: | ---: | --- | --- | --- | --- | --- |",
  ];
  for (const report of reports) {
    lines.push(`| ${cell(report.file)} | ${report.modelCalls} | ${report.toolCalls} | ${total(report.promptTokens)} | ${total(report.cachedTokens)} | ${total(report.uncachedPromptTokens)} | ${total(report.cacheHitRate, true)} | ${total(report.outputOverflows)} |`);
  }
  for (const report of reports) {
    lines.push("", `## ${cell(report.file)}`, "",
      `Requests below ${percent(report.below)}, plus unknown / zero-input rates (lowest known rates first).`,
      "", "| Line | Execution | Run | Model step | Request | Model | Input | Cached | Non-cache | Hit rate | Status |",
      "| ---: | --- | --- | --- | --- | --- | ---: | ---: | ---: | --- | --- |");
    for (const request of report.requests) {
      lines.push(`| ${[request.line, request.executionId, request.runId, request.modelStepId, request.requestId, request.model, request.inputTokens, request.cachedTokens, request.uncachedTokens, percent(request.cacheHitRate), request.rateStatus].map(cell).join(" | ")} |`);
    }
    if (!report.requests.length) lines.push("", "No matching requests.");
  }
  return lines.join("\n") + "\n";
}
