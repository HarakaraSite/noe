// Use a URL alias and a direct dynamic import in the same runtime.
import * as mapped from "lodash";

const direct = await import("https://esm.sh/lodash-es@4.17.21");
if (mapped !== direct) {
  throw new Error("The alias and direct URL must use the same module");
}
console.log(JSON.stringify(mapped.chunk([1, 2, 3, 4, 5], 2)));
