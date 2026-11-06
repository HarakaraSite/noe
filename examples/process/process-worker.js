// Exchange several requests with one persistent NOE child process.
import { spawn } from "noe:process";
import { resolve, dirname, join } from "node:path";

const child = await spawn(resolve(process.argv[0]), [
  "run", join(dirname(process.argv[1]), "stdio-jsonl.js"),
], { stderr: "inherit" });

const writer = child.stdin.getWriter();
const encoder = new TextEncoder();
try {
  // WritableStream serializes these writes; the same child handles all three.
  await Promise.all([1, 2, 3].map(id =>
    writer.write(encoder.encode(JSON.stringify({ id, value: `message ${id}` }) + "\n"))
  ));
  await writer.close();
  const responses = await new Response(child.stdout).text();
  const status = await child.exited;
  if (status.code !== 0) throw new Error(JSON.stringify(status));
  console.log(responses.trimEnd());
} finally {
  await child.close();
}
