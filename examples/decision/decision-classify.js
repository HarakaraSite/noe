import { decide } from "noe:decision";

const state = process.argv[2] || "Checkout fails after I submit a payment.";
const model = process.argv[3] || "typesafe/jev-1.14";
const result = await decide({
  provider: "openrouter",
  model,
  state,
  questions: {
    team: {
      type: "choice",
      instructions: "Which team should handle this report?",
      criteria: {
        payments: "Payment or checkout issues",
        account: "Account or login issues",
        other: "Other issues",
      },
    },
  },
});
console.log(JSON.stringify(result, null, 2));
