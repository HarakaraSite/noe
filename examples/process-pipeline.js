// Pass bytes through two child processes with bounded stream buffering.
import { spawn } from "noe:process";
import { stdin, stdout, pipe } from "noe:io";
import { resolve } from "node:path";

const code = `import {stdin,stdout,pipe} from "noe:io"; await pipe(stdin,stdout);`;
const first = await spawn(resolve(process.argv[0]), ["eval", code], { stderr: "inherit" });
let second;
try {
  second = await spawn(resolve(process.argv[0]), ["eval", code], { stderr: "inherit" });
  const [a, b] = await Promise.all([
    first.exited,
    second.exited,
    pipe(stdin, first.stdin),
    pipe(first.stdout, second.stdin),
    pipe(second.stdout, stdout, { preventClose: true }),
  ]);
  if (a.code !== 0 || b.code !== 0) throw new Error(JSON.stringify({ a, b }));
} finally {
  await Promise.all([first.close(), second?.close()]);
}
