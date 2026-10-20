// Deliberately incomplete: the workflow will repair this implementation.
export function sumPositiveIntegers(values) {
  if (!Array.isArray(values)) throw new TypeError("values must be an array");
  return values.reduce((sum, value) => typeof value === "number" && value > 0 ? sum + value : sum, 0);
}
