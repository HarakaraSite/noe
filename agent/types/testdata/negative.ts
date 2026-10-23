import { list_models, run_agent } from "noe:agent";
import { generate } from "noe:model";

// @ts-expect-error read_image is exported by noe:image, not noe:agent
import { read_image } from "noe:agent";

// @ts-expect-error invalid agent effort must be caught
run_agent({provider: "openrouter", model: "provider/model", effort: "urgent"}, "task");

// @ts-expect-error model.generate requires an explicit provider
generate({model: "provider/model", messages: [{role: "user", content: "Hello"}]});

// @ts-expect-error supported detail values are auto, low, and high
import("noe:image").then(({read_image}) => read_image("diagram.png", {detail: "ultra"}));

// @ts-expect-error list_models returns model metadata, not strings
const wrongResult: string[] = await list_models();

declare const context: NOEAgent.AgentContextSnapshot;

if (context.budget.inputTokens !== undefined) {
  // @ts-expect-error checking only undefined must not erase the actual null case
  context.budget.inputTokens.toFixed();
}

if (context.modelWindow.tokens !== undefined) {
  // @ts-expect-error checking only undefined must not erase the actual null case
  context.modelWindow.tokens.toFixed();
}

if (context.modelWindow.source !== undefined) {
  // @ts-expect-error checking only undefined must not erase the actual null case
  context.modelWindow.source.toUpperCase();
}

// @ts-expect-error internal bridge modules are not part of the public type surface
import("noe:agent/internal").then(({request}) => request("method", "{}"));

void wrongResult;
