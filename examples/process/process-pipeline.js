// Pass bytes through two child processes with bounded stream buffering.
import { spawn } from "node:child_process";
import { stdin, stdout } from "node:process";
import { resolve } from "node:path";

const code = `import {stdin,stdout} from "node:process"; await stdin.pipeTo(stdout);`;
const first = await spawn(resolve(process.argv[0]), ["eval", code], { stderr: "inherit" });
let second;
try {
  second = await spawn(resolve(process.argv[0]), ["eval", code], { stderr: "inherit" });
  const [a, b] = await Promise.all([
    first.exited,
    second.exited,
    stdin.pipeTo(first.stdin),
    first.stdout.pipeTo(second.stdin),
    second.stdout.pipeTo(stdout, { preventClose: true }),
  ]);
  if (a.code !== 0 || b.code !== 0) throw new Error(JSON.stringify({ a, b }));
} finally {
  await Promise.all([first.close(), second?.close()]);
}
