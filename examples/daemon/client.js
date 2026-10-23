// Send one JSONL request to the loopback daemon and consume its reply.
import { connect } from "noe:sockets";

const port = Number(process.argv[2] ?? 9070);
const request = JSON.parse(process.argv[3] ?? '{"id":1,"op":"echo","text":"hello 東京"}');
const connection = await connect({ network: "tcp", host: "127.0.0.1", port });
try {
  const writer = connection.writable.getWriter();
  try {
    await writer.write(new TextEncoder().encode(JSON.stringify(request) + "\n"));
    await writer.close();
  } finally {
    writer.releaseLock();
  }
  const text = await new Response(connection.readable).text();
  const result = JSON.parse(text);
  console.log(JSON.stringify(result));
  if (!result.ok) process.exitCode = 1;
} finally {
  await connection.close();
}
