// Import a public ESM library without a local download or import map.
import { chunk } from "https://esm.sh/lodash-es@4.17.21";

console.log(JSON.stringify(chunk([1, 2, 3, 4, 5], 2)));
