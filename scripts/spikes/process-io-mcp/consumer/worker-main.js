import { Worker } from "node:worker_threads";

const config = JSON.parse(process.argv[2]);
const worker = new Worker(process.argv[3], {
  type: "module",
  workerData: { config },
});
const result = await new Promise((resolve, reject) => {
  worker.once("message", resolve);
  worker.once("error", reject);
});
await worker.terminate();
if (result.error) throw new Error(result.error);
console.log(JSON.stringify(result));
