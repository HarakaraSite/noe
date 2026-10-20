import { sumPositiveIntegers } from "./solution.js";
const cases = [
  ["empty", [], 0],
  ["mixed signs", [1, -2, 3, 0], 4],
  ["nonintegers ignored", [1, 1.5, "2", null, NaN, Infinity], 1],
  ["all negative", [-5, -1], 0],
];
const results = cases.map(([name, input, expected]) => {
  const actual = sumPositiveIntegers(input);
  return {name, passed: actual === expected, expected, actual: String(actual)};
});
let rejected = false;
try { sumPositiveIntegers("not array"); } catch (error) { rejected = error instanceof TypeError; }
results.push({name: "reject nonarray", passed: rejected});
console.log(JSON.stringify({passed: results.every(result => result.passed), results}));
if (results.some(result => !result.passed)) process.exit(1);
