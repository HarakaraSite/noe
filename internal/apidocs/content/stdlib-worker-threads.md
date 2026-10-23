# node:worker_threads

### API information

Reference: `node:worker_threads`; aliases: `worker_threads`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Messaging exports are present in the stdlib profile; starting a Worker requires Profile.Modules.Workers. Worker entries use their own runtime and loader.

Public-definition source: internal/stdlib/worker.go. Reading this reference evaluates no target module and makes no provider request.

Runs JavaScript in additional QuickJS runtimes and exchanges structured-clone messages through ports. NOE enables Workers. Each Worker has its own runtime, loader, and resource lifetime.

### Exports

`BroadcastChannel`, `MessageChannel`, `MessagePort`, `SHARE_ENV`, `Worker`, `default`, `getEnvironmentData`, `isMainThread`, `isMarkedAsUntransferable`, `markAsUntransferable`, `parentPort`, `receiveMessageOnPort`, `resourceLimits`, `setEnvironmentData`, `threadId`, `workerData`

In a Worker, parentPort and workerData describe that Worker; on the main thread both are null. NOE accepts local .js/.mjs files or inline source with eval: true, and blocks agent-control modules.

## Worker

Syntax: new Worker(filename, options = {}). In NOE, a file name must be relative (./ or ../), absolute, or a local file: URL. options.workerData is structured-cloned into workerData; options.argv, options.env, options.name, and options.transferList configure the Worker. With eval: true, filename is source text. The returned EventEmitter-like Worker emits online, message, error, and exit.

The Node-style constructor ignores options.type in this pinned version. With eval: true, source runs as a script when it parses as one; module-only syntax, such as static imports or top-level await, is detected and evaluated as an ES module. options.type does not override this selection. NOE loads local .js/.mjs Worker files as ES modules through its file loader. The global Web Worker constructor has a separate type option; do not transfer its option contract to node:worker_threads.Worker.

~~~js
import { Worker } from "node:worker_threads";

const worker = new Worker(
  'import { parentPort, workerData } from "node:worker_threads"; parentPort.postMessage(workerData.value * 2);',
  { eval: true, workerData: { value: 21 } },
);
const answer = await new Promise((resolve, reject) => {
  worker.once("message", resolve);
  worker.once("error", reject);
});
console.log(answer);
~~~

A referenced Worker keeps its parent runtime alive. NOE terminates Workers when the parent closes. Worker imports use NOE's local loader. Imports of noe:agent, noe:agent/api, noe:agent/hooks, noe:decision, noe:image, noe:auth, noe:agent/task, and noe:agent/history fail inside Workers. NOE does install its native I/O APIs there.

## parentPort

parentPort is the Worker-side MessagePort connected to the parent, or null on the main thread. Use postMessage(value, transferList?) to send structured-clone data and listen for message events to receive the parent's data.

~~~js
import { parentPort } from "node:worker_threads";

parentPort.postMessage({ result: 42 });
~~~

Transferred ports change ownership: the sending side's port is detached and the receiving side owns that endpoint. Worker ports close when its runtime ends.

## workerData

workerData is the structured-clone copy of options.workerData passed to the Worker constructor. It is null on the main thread.

~~~js
import { workerData, parentPort } from "node:worker_threads";

parentPort.postMessage(workerData.value * 2);
~~~

Only cloneable values and explicitly transferred ports cross the runtime boundary.

## MessageChannel

new MessageChannel() -> {port1, port2} creates connected MessagePort endpoints. Each endpoint sends with postMessage and receives through a message event or onmessage. Call close() when an endpoint is no longer needed; a transferred port is owned by the receiver.

~~~js
const { port1, port2 } = new MessageChannel();
port2.onmessage = (event) => {
  console.log(event.data.value);
  port1.close();
  port2.close();
};
port1.postMessage({ value: 42 });
~~~

## MessagePort

A MessagePort sends and receives structured-clone messages. Use postMessage(value, transferList?), addEventListener("message", handler) or onmessage, and close(). start() is available for event-style delivery. receiveMessageOnPort(port) returns a queued message as {message}, or undefined. Values that cannot be cloned or transferred cause an error.

~~~js
import { MessageChannel } from "node:worker_threads";

const { port1, port2 } = new MessageChannel();
port2.onmessage = (event) => {
  console.log(event.data);
  port1.close();
  port2.close();
};
port1.postMessage("hello");
~~~

## terminate

await worker.terminate() -> exitCode or undefined. It stops an active Worker and resolves to its exit code when its runtime has ended. If the Worker has already exited, it resolves immediately to undefined. Closing the parent runtime also terminates its Workers.

~~~js
import { Worker } from "node:worker_threads";

const worker = new Worker("await new Promise(resolve => setTimeout(resolve, 10000))", { eval: true });
const exitCode = await worker.terminate();
console.log(exitCode);
~~~

### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
