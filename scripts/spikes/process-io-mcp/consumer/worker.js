import { parentPort, workerData } from "node:worker_threads";
import { exercise } from "mcp/exercise";

try {
  const result = await exercise(workerData.config);
  if (result.largeInputBytes !== 270000) throw new Error("UTF-8 input length mismatch");
  parentPort.postMessage({ route: "worker", ...result });
} catch (error) {
  parentPort.postMessage({ error: String(error?.stack || error) });
}
parentPort.close();
