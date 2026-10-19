// Wait without an auxiliary timer; close the subscription after one signal.
import { subscribe } from "noe:signals";

const name = process.argv[2] ?? "SIGTERM";
const subscription = subscribe(name);
console.log(`pid=${process.pid}, waiting for ${name}`);
try {
  for await (const received of subscription) {
    console.log(`received ${received}`);
    break;
  }
} finally {
  await subscription.close();
}
