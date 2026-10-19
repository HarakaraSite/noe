// Read an index, exchange bytes over a socket, and update the selected range.
import { open } from "noe:fs-handle";
import { connect, listen } from "noe:sockets";
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const directory = mkdtempSync(join(tmpdir(), "noe-io-"));
const path = join(directory, "index.bin");
writeFileSync(path, new Uint8Array([0, 1, 2, 3, 4, 5, 6, 7]));
const network = process.argv[2] ?? "tcp";
const options = network === "unix"
  ? { network, path: join(directory, "service.sock") }
  : { network, host: "127.0.0.1", port: 0 };
const file = await open(path, { mode: "r+" });
let listener;
try {
  listener = await listen(options);
  const service = (async () => {
    const connection = await listener.accept();
    try {
      const data = new Uint8Array(await new Response(connection.readable).arrayBuffer());
      const writer = connection.writable.getWriter();
      await writer.write(data.map(byte => byte + 10));
      await writer.close();
    } finally { await connection.close(); }
  })();
  const connection = await connect(network === "tcp"
    ? { network, host: "127.0.0.1", port: listener.address.port } : options);
  try {
    const { data } = await file.readAt(2, 3);
    const writer = connection.writable.getWriter();
    await writer.write(data);
    await writer.close();
    const response = new Uint8Array(await new Response(connection.readable).arrayBuffer());
    await file.writeAt(2, response);
  } finally { await connection.close(); }
  await service;
  console.log(Array.from(readFileSync(path))); // [0, 1, 12, 13, 14, 5, 6, 7]
} finally {
  if (listener) await listener.close();
  await file.close();
  rmSync(directory, { recursive: true });
}
