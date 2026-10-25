// Fail optional recording while leaving the task free to finish.
import { writeFileSync } from "node:fs";

export function beforeModelRequest() {
  writeFileSync("./missing-directory/output.txt", "optional record");
}

export function afterModelResponse() {
  throw new Error("final hook fixture failed");
}
