import {readFile} from "noe:fs";
import {connect, listen} from "noe:sockets";

const [certificatePath, privateKeyPath] = process.argv.slice(2);
if (!certificatePath || !privateKeyPath || process.argv.length !== 4) {
  throw new Error("usage: noe run examples/low-level/tls-roundtrip.js <certificate.pem> <key.pem>");
}

const [certificate, privateKey] = await Promise.all([
  readFile(certificatePath, "utf8"),
  readFile(privateKeyPath, "utf8"),
]);

const text = new TextEncoder().encode("hello over TLS");
const binary = new Uint8Array([0, 255, 127]);
const expected = new Uint8Array([...text, ...binary]);
let listener;
let client;
let serverTask;

try {
  listener = await listen({
    network: "tls",
    host: "127.0.0.1",
    port: 0,
    cert: certificate,
    key: privateKey,
  });

  // Start accept before connect so the TLS server handshake is being handled.
  const pendingAccept = listener.accept();
  serverTask = (async () => {
    const connection = await pendingAccept;
    try {
      const request = new Uint8Array(await new Response(connection.readable).arrayBuffer());
      const writer = connection.writable.getWriter();
      try {
        await writer.write(request);
        await writer.close();
      } finally {
        writer.releaseLock();
      }
    } finally {
      await connection.close();
    }
  })();
  // Keep a server-side failure observed while the client is exchanging data.
  void serverTask.catch(() => {});

  client = await connect({
    network: "tls",
    host: listener.address.host,
    port: listener.address.port,
    ca: certificate,
  });

  const writer = client.writable.getWriter();
  try {
    await writer.write(text);
    await writer.write(binary);
    await writer.close(); // Send close_notify while keeping the read side open.
  } finally {
    writer.releaseLock();
  }

  const response = new Uint8Array(await new Response(client.readable).arrayBuffer());
  await serverTask;
  if (response.length !== expected.length || response.some((byte, index) => byte !== expected[index])) {
    throw new Error("TLS echo did not match the text and binary payload");
  }

  console.log("TLS roundtrip passed");
} finally {
  try {
    if (client) await client.close();
  } finally {
    try {
      if (listener) await listener.close();
    } finally {
      if (serverTask) await serverTask.catch(() => {});
    }
  }
}
