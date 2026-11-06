import {serve} from "noe:http";

const encoder = new TextEncoder();
const server = serve({hostname: "127.0.0.1", port: 0}, () => new Response(
  new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode("id: 1\ndata: hello\n\n"));
      controller.enqueue(encoder.encode("event: done\ndata: completed\n\n"));
      controller.close();
    },
  }),
  {headers: {"Content-Type": "text/event-stream"}},
));

const source = new EventSource(server.url);
try {
  await new Promise((resolve, reject) => {
    source.onmessage = event => console.log(event.lastEventId, event.data);
    source.addEventListener("done", event => {
      console.log(event.type, event.data);
      source.close();
      resolve();
    });
    source.onerror = () => {
      source.close();
      reject(new Error("EventSource connection failed"));
    };
  });
} finally {
  source.close();
  await server.close();
}
