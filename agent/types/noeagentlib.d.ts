/**
 * Type declarations for the public JavaScript modules installed by noeagentlib.
 *
 * `noe:agent`, `noe:model`, `noe:decision`, and `noe:image` are documented as
 * main-runtime APIs and require the corresponding host binding. `noe:auth`
 * and `noe:agent-log` require their public Go bindings (`agent/auth.Bind` and
 * `agent/log.Bind`). These declarations do not imply that an API is enabled
 * by a particular Profile or available in a particular Worker/runtime.
 *
 * This file describes only public module exports. Private `noe:* /internal`
 * modules and implementation helpers are intentionally omitted.
 */
declare namespace NOEAgent {
  /** JSON-compatible data accepted or returned by APIs that preserve JSON. */
  type JSONValue =
    | string
    | number
    | boolean
    | null
    | readonly JSONValue[]
    | { readonly [key: string]: JSONValue };

  type JSONRecord = { readonly [key: string]: JSONValue };

  type Effort = "auto" | "none" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max";
  type ModelKind = "agent" | "decision" | "image";
  type TaskState = "completed" | "failed" | "cancelled";

  interface ExecutionLimits {
    /** Nonnegative safe integer. Omitted dimensions are unlimited. */
    providerCalls?: number;
    /** Nonnegative safe integer. Omitted dimensions are unlimited. */
    agentStarts?: number;
    /** Nonnegative safe integer. Omitted dimensions are unlimited. */
    queuedTasks?: number;
    /** Nonnegative safe integer. Omitted dimensions are unlimited. */
    hookCalls?: number;
  }

  interface ContextBudget {
    /** Positive safe integer. */
    inputTokens: number;
  }

  interface ContextPolicy {
    /** Finite number strictly between 0 and 1. */
    retainRatio?: number;
    /** Positive safe integer. */
    responseReserveTokens?: number;
  }

  /** Fields accepted by run_agent/spawn_agent; omitted values may inherit in a child. */
  interface AgentConfig {
    provider?: string;
    model?: string;
    effort?: Effort;
    name?: string;
    system?: string;
    /** Nonnegative integer; zero leaves the provider default. */
    maxTokens?: number;
    /** Nonnegative integer; zero means no per-task model-call limit. */
    maxSteps?: number;
    cacheKey?: string;
    hooks?: readonly string[];
    contextBudget?: ContextBudget | null;
    contextPolicy?: ContextPolicy | null;
    /** Root-only shared group limits. */
    executionLimits?: ExecutionLimits;
  }

  interface RunAgentOptions {
    keepAlive?: boolean;
    allowDelegation?: boolean;
  }

  interface ModelQuery {
    provider?: string;
    kind?: ModelKind;
  }

  interface ModelInfo {
    provider: string;
    model: string;
    kind: ModelKind;
    description?: string;
    tags?: readonly string[];
    capabilities?: Readonly<Record<string, boolean>>;
    contextWindow?: number;
    maxOutputTokens?: number;
    efforts?: readonly string[];
  }

  interface CommandReceipt {
    commandId: string;
    status: string;
  }

  interface SpawnAgentResult {
    agentId: string;
    /** Absent when the instance is started idle. */
    commandId?: string;
  }

  interface ImageErrorDetails {
    code: string;
    message: string;
    providerCode?: string;
    providerMessage?: string;
    statusCode?: number;
    requestId?: string;
    usage?: JSONValue;
  }

  interface TaskOutcome {
    runId: string;
    commandId: string;
    state: TaskState;
    /** Omitted when wait_task/wait_tasks/observe_agent requests includeAnswer: false. */
    answer?: string;
    /** UTF-8 byte length when answer is omitted. */
    answerBytes?: number;
    error?: string;
    errorDetails?: ImageErrorDetails;
  }

  interface TaskReference {
    agentId: string;
    commandId: string;
  }

  interface WaitTasksOptions {
    tasks: readonly TaskReference[];
    includeAnswer?: boolean;
  }

  interface CompletedTask {
    agentId: string;
    outcome: TaskOutcome;
  }

  interface WaitTasksResult {
    /** A steer return does not consume guidance or cancel any task. */
    reason: "completed" | "steer";
    /** The caller's owner run, when the caller belongs to an active run. */
    runId?: string;
    completed: CompletedTask[];
    pending: TaskReference[];
  }

  interface ReadTaskResult {
    runId: string;
    commandId: string;
    state: TaskState;
    text: string;
    offset: number;
    nextOffset: number;
    totalBytes: number;
    eof: boolean;
  }

  interface ActivityOrigin {
    kind: string;
    runId?: string;
    agentId?: string;
    toolCallId?: string;
    toolName?: string;
    commandId?: string;
    hookId?: string;
    module?: string;
    phase?: string;
    modelStepId?: string;
  }

  interface ExecutionCounters {
    providerCalls: number;
    agentProviderCalls: number;
    decisionProviderCalls: number;
    imageProviderCalls: number;
    agentStarts: number;
    queuedTasks: number;
    hookCalls: number;
    activeAgents: number;
    activeTasks: number;
  }

  interface ExecutionUsage {
    promptTokens?: number;
    completionTokens?: number;
    totalTokens?: number;
    cachedTokens?: number;
    cost?: number;
  }

  interface ExecutionStop {
    reason: string;
    limitName: string;
    limit: number;
    used: number;
    source?: {
      activityId?: string;
      origin?: ActivityOrigin;
    };
  }

  interface ExecutionSnapshot {
    groupId: string;
    counters: ExecutionCounters;
    limits: ExecutionLimits;
    elapsedMs: number;
    stop?: ExecutionStop;
    usage?: ExecutionUsage;
  }

  interface ContextTrimSnapshot {
    id: string;
    beforeTokens: number;
    afterTokens: number;
    removedBlocks: number;
  }

  interface ExecutionContextSnapshot {
    policy: string;
    budgetSource: string;
    fixedTokens: number;
    transientTokens: number;
    responseReserveTokens: number | null;
    responseReserveUnknown: boolean;
    estimatedTokens: number;
    budgetTokens: number;
    totalInputBudgetTokens: number;
    usedPercent: number | null;
    nearBudget: boolean;
    budgetExceeded: boolean;
    targetUnreachable: boolean;
    trimCount: number;
    lastTrim: ContextTrimSnapshot | null;
    pendingTrim?: ContextTrimSnapshot;
  }

  interface ContextPrevious {
    requestId: string;
    status: string;
    promptTokens: number | null;
    cachedTokens: number | null;
    measuredAt: string;
  }

  interface ContextEstimate {
    tokens: number;
    imageTokens?: number;
    method: string;
    stage: string;
    modelStepId: string;
    requestId: string | null;
    estimatedAt: string;
  }

  interface AgentContextSnapshot {
    executionContext?: ExecutionContextSnapshot;
    runId: string;
    modelStepId: string;
    budget: { inputTokens: number | null };
    modelWindow: { tokens: number | null; source: string | null };
    previous: ContextPrevious | null;
    estimate: ContextEstimate | null;
    remainingTokens: number | null;
    usedPercent: number | null;
    updatedAt: string;
  }

  interface AgentSnapshot {
    agentId: string;
    groupId?: string;
    activityId?: string;
    causedBy?: readonly string[];
    origin?: ActivityOrigin;
    execution?: ExecutionSnapshot;
    parentRunId: string;
    name: string;
    state: string;
    reason: string;
    keepAlive: boolean;
    currentRunId: string;
    currentCommandId?: string;
    queueLength: number;
    lastResult: TaskOutcome | null;
    context?: AgentContextSnapshot;
  }

  interface DecisionOptions {
    provider: "openrouter";
    model: string;
    /** A nonempty string, JSON object, or JSON array. */
    state: string | JSONRecord | readonly JSONValue[];
    /** Nonempty map; every question value must be a JSON object. */
    questions: Readonly<Record<string, JSONRecord>>;
  }

  /** Provider-defined answer fields are retained as JSON values. */
  type DecisionResponse = JSONRecord & {
    answers: Readonly<Record<string, JSONValue>>;
  };

  interface ImageMetadata {
    /** Present when the image is attached to a run_js model request. */
    imageId?: string;
    path: string;
    mimeType: string;
    bytes: number;
    width: number;
    height: number;
    detail?: string;
  }

  interface ReadImageOptions {
    detail?: "auto" | "low" | "high";
  }

  interface GenerateImageOptions {
    provider: string;
    model: string;
    prompt: string;
    output: string;
    /** Provider-specific JSON parameters; transport and identity fields are reserved. */
    parameters?: JSONRecord;
  }

  interface GeneratedImage extends ImageMetadata {
    provider: string;
    model: string;
    responseModel?: string;
    requestId?: string;
    usage?: JSONValue;
  }

  interface ModelGenerateOptions {
    provider: string;
    runId?: string;
    sessionId?: string;
    model: string;
    /** Provider-compatible message objects; at least one message is required. */
    messages: readonly JSONRecord[];
    tools?: readonly JSONRecord[];
    cacheKey?: string;
    /** Nonnegative integer. */
    maxTokens?: number;
    effort?: string;
  }

  interface ModelGenerateResponse {
    /** Provider-compatible assistant message object. */
    message: JSONRecord;
    finishReason: string;
    usage?: JSONValue;
  }

  interface AuthTokens {
    access_token: string;
    refresh_token?: string;
    id_token: string;
    token_type: string;
    expires_in: number;
    scope: string;
  }

  interface SaveChatGPTCredentialsOptions {
    file: string;
    clientId: string;
    hostId: string;
    nonce: string;
    tokens: AuthTokens;
  }

  interface SavedAccount {
    file: string;
    email?: string;
    /** ISO 8601 timestamp. */
    expiresAt: string;
  }

  interface LogCursor {
    executionId: string;
    seq: number;
  }

  interface AnalyzeLogOptions {
    runId: string;
    cursor?: LogCursor;
    includeThinking?: boolean;
    includeOutput?: boolean;
  }

  interface LogEvent extends JSONRecord {
    type: string;
    executionId: string;
    seq: number;
  }

  interface LogUsage {
    promptTokens?: number;
    completionTokens?: number;
    totalTokens?: number;
    cachedTokens?: number;
    cost?: number;
  }

  interface DiagnosticPosition {
    source?: string;
    /** One-based; omitted when not supplied by the engine. */
    line?: number;
    /** One-based; omitted when not supplied by the engine. */
    column?: number;
  }

  interface EncodedBytes {
    encoding: "base64";
    data: string;
  }

  interface ExecutionDiagnostic {
    code: "execution_error" | "serialization_error" | "cancelled" | "timeout" | "cleanup_error";
    stage: "setup" | "execute" | "serialize" | "cleanup";
    message: string;
    stack?: string;
    position?: DiagnosticPosition;
    exception?: {
      code?: string | number | boolean;
      signal?: string | number | boolean;
      stdout?: string | number | boolean | EncodedBytes;
      stderr?: string | number | boolean | EncodedBytes;
    };
  }

  /** Raw runner result before an afterTool hook's optional arbitrary replacement. */
  interface RunJSResult {
    stdout: string;
    stderr: string;
    exitCode: number;
    reason: "completed" | "failed" | "timeout" | "cancelled";
    data?: JSONValue;
    diagnostics?: readonly ExecutionDiagnostic[];
    stdoutOmittedBytes?: number;
    stderrOmittedBytes?: number;
  }

  interface LogToolResult {
    toolCallId: string;
    success: boolean;
    exitCode: number;
    reason?: string;
    duration?: number;
    text?: string;
    /** Present only when includeOutput was requested; null differs from absence. */
    data?: JSONValue;
    /** Diagnostics remain available independently of captured-output selection. */
    diagnostics?: readonly ExecutionDiagnostic[];
  }

  interface LogSnapshot {
    groupId?: string;
    execution?: JSONValue;
    context?: JSONValue;
    agentId?: string;
    runId: string;
    parentRunId: string;
    name: string;
    model: string;
    status: string;
    phase: string;
    action: string;
    lastMessage: string;
    finalText?: string;
    error?: string;
    lastToolResult?: LogToolResult;
    usage?: LogUsage;
    updatedAt: string;
    cursor: LogCursor;
    events: readonly LogEvent[];
  }
}

declare module "noe:agent" {
  /** Start an agent task and wait for its instance to stop and clean up. */
  export function run_agent(
    config: NOEAgent.AgentConfig | undefined,
    instruction: string,
    options?: NOEAgent.RunAgentOptions,
  ): Promise<NOEAgent.AgentSnapshot>;

  /** Start an agent instance and return after launch/task acceptance. */
  export function spawn_agent(
    config: NOEAgent.AgentConfig | undefined,
    instruction: string,
    options?: NOEAgent.RunAgentOptions,
  ): Promise<NOEAgent.SpawnAgentResult>;
  /** Start an idle keep-alive instance. */
  export function spawn_agent(
    config: NOEAgent.AgentConfig | undefined,
    instruction: undefined,
    options: NOEAgent.RunAgentOptions & { keepAlive: true },
  ): Promise<NOEAgent.SpawnAgentResult>;

  export function observe_agent(options: {
    agentId: string;
    includeAnswer?: boolean;
  }): Promise<NOEAgent.AgentSnapshot>;

  export function queue_agent(options: {
    agentId: string;
    instruction: string;
    commandId?: string;
  }): Promise<NOEAgent.CommandReceipt>;

  export function wait_task(options: {
    agentId: string;
    commandId: string;
    includeAnswer?: boolean;
  }): Promise<NOEAgent.TaskOutcome>;

  /** Wait for a whole task set, or return control on guidance to the owner run. */
  export function wait_tasks(options: NOEAgent.WaitTasksOptions): Promise<NOEAgent.WaitTasksResult>;

  export function read_task_result(options: {
    agentId: string;
    commandId: string;
    offset?: number;
    maxBytes?: number;
  }): Promise<NOEAgent.ReadTaskResult>;

  export function steer_agent(options: {
    runId: string;
    instruction: string;
    commandId?: string;
  }): Promise<NOEAgent.CommandReceipt>;

  export function cancel_task(options: {
    agentId: string;
    commandId: string;
  }): Promise<{ agentId: string; commandId: string; status: string }>;

  export function stop_agent(options: {
    agentId: string;
  }): Promise<{ agentId: string; status: string }>;

  export function list_models(options?: NOEAgent.ModelQuery): Promise<NOEAgent.ModelInfo[]>;
}

declare module "noe:decision" {
  export function decide(options: NOEAgent.DecisionOptions): Promise<NOEAgent.DecisionResponse>;
}

declare module "noe:image" {
  export function read_image(path: string, options?: NOEAgent.ReadImageOptions): Promise<NOEAgent.ImageMetadata>;
  export function generate_image(options: NOEAgent.GenerateImageOptions): Promise<NOEAgent.GeneratedImage>;
}

declare module "noe:model" {
  export function generate(options: NOEAgent.ModelGenerateOptions): Promise<NOEAgent.ModelGenerateResponse>;
  export function list_models(options?: NOEAgent.ModelQuery): Promise<NOEAgent.ModelInfo[]>;
  export { decide } from "noe:decision";
  export { generate_image } from "noe:image";
}

declare module "noe:auth" {
  export function save_chatgpt_credentials(
    options: NOEAgent.SaveChatGPTCredentialsOptions,
  ): Promise<NOEAgent.SavedAccount>;
}

declare module "noe:agent-log" {
  export function analyze_log(
    input: string | Uint8Array,
    options: NOEAgent.AnalyzeLogOptions,
  ): NOEAgent.LogSnapshot;
}
