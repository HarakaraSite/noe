# `noe:decision`

### API information

Public exports: `decide`.

Reference: `noe:decision`. Owner: noeagentlib.

Type declarations: `agent/types/noeagentlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires the model/decision binding in the main runtime. Provider calls follow the configured service and execution scope.

Public-definition source: internal/decision/decision.go, internal/modelbinding/binding.go. Reading this reference evaluates no target module and makes no provider request.

`noe:decision` exports one operation, `decide`. It makes a structured provider decision and returns the provider response as JSON; it does not start an agent loop. It is available in the main runtime, not Workers. When called from a managed task, the request belongs to that task's owner and cancellation scope and counts toward its shared provider-call limit.

## decide

```js
import { decide } from "noe:decision";
```

`decide({provider, model, state, questions}) -> Promise<DecisionResponse>` requires all four fields:

When called from a managed task, the request belongs to that task's owner and cancellation scope and consumes its shared provider-call limit.

- `provider` must be `"openrouter"`.
- `model` must be a nonempty model ID. NOE does not infer a decision model; select it explicitly, for example from `list_models({kind: "decision"})`.
- `state` is a nonempty string, JSON object, or JSON array.
- `questions` is a nonempty object whose values are question objects. Question types and fields are interpreted by the selected decision model API.

Inputs must contain only finite JSON values, plain JSON objects and arrays, and no cycles. Unknown top-level options, invalid types, or unsupported provider values throw `TypeError`. Provider or response failures reject the Promise. A successful response is the provider's JSON object; it includes an `answers` object, and provider-defined fields are preserved. Read a named answer from `response.answers` and handle its provider-defined shape.

```js
import { decide } from "noe:decision";

const response = await decide({
  provider: "openrouter",
  model: "provider/decision-model",
  state: {text: "The file contains a JSON array of customer records."},
  questions: {
    format: {
      type: "choice",
      instructions: "Classify the input format.",
      criteria: {json: "Valid JSON", other: "Anything else"},
    },
  },
});
console.log(response.answers.format);
```

The returned object is the full provider response. Its answer fields, optional probabilities/confidence, model and usage metadata can vary by provider API and model; do not assume fields that were not returned.
