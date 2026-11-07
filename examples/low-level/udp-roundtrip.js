import {bind} from "noe:sockets";

const encoder = new TextEncoder();
const expected = [
  encoder.encode("hello over UDP"),
  new Uint8Array([0, 255, 127, 34]),
  new Uint8Array(),
];
const sameBytes = (left, right) =>
  left.length === right.length && left.every((byte, index) => byte === right[index]);
const sameAddress = (left, right) =>
  left.network === right.network && left.host === right.host && left.port === right.port;

const controller = new AbortController();
const timeout = setTimeout(() => {
  controller.abort(new Error("UDP roundtrip timed out"));
}, 5000);
let server;
let client;
let serverTask;

try {
  server = await bind({network: "udp", host: "127.0.0.1", port: 0});
  client = await bind({network: "udp", host: "127.0.0.1", port: 0});

  serverTask = (async () => {
    for (let index = 0; index < expected.length; index++) {
      const packet = await server.receive({signal: controller.signal});
      if (!sameAddress(packet.remoteAddress, client.address)) {
        throw new Error("server received an unexpected source address");
      }
      await server.send(packet.data, packet.remoteAddress, {signal: controller.signal});
    }
  })();
  void serverTask.catch(error => {
    if (!controller.signal.aborted) controller.abort(error);
  });

  for (const data of expected) {
    await client.send(data, server.address, {signal: controller.signal});
  }

  const remaining = expected.slice();
  for (let index = 0; index < expected.length; index++) {
    const packet = await client.receive({signal: controller.signal});
    if (!sameAddress(packet.remoteAddress, server.address)) {
      throw new Error("client received an unexpected source address");
    }
    const match = remaining.findIndex(data => sameBytes(data, packet.data));
    if (match < 0) throw new Error("UDP reply did not match a request packet");
    remaining.splice(match, 1);
  }

  await serverTask;
  if (remaining.length !== 0) throw new Error("one or more UDP replies were missing");
  console.log("UDP roundtrip passed");
} finally {
  clearTimeout(timeout);
  if (!controller.signal.aborted) controller.abort(new Error("UDP roundtrip finished"));
  try {
    await Promise.all([client?.close(), server?.close()]);
  } finally {
    if (serverTask) await serverTask.catch(() => {});
  }
}
