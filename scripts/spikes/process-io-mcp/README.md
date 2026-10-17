# Official MCP SDK with a NOE process transport

This spike separates the official SDK `Client` export from the NOE transport. It pins `@modelcontextprotocol/sdk` **1.32.1** and esbuild **0.25.12**. The generated bundle exports the SDK client; [`noe-transport.js`](noe-transport.js) remains a normal ESM module that imports `spawn` from `noe:process`. No SDK patch or product MCP API is involved.

The SDK's local `shared/transport.d.ts` contract is `start()`, `send(message, options?)`, and `close()`, with `onmessage`, `onerror`, and `onclose` callbacks. The transport uses newline-delimited JSON on piped standard streams. Its caller supplies `command`, `args`, and `cwd`; it does not infer either the NOE executable or server entry from `process.argv`. EOF, malformed JSON, stream errors, and child startup failure notify `onerror` and `onclose`, allowing the SDK to reject pending RPCs. Normal close sends stdin EOF, waits briefly for `child.exited`, and calls `child.close()` for cleanup; fatal paths also close the child.

## Prepare the reusable client bundle

From the repository root, prepare the pinned dependencies and ESM bundle in a user-selected temporary directory:

```sh
go build -o /tmp/noe-process-io ./cmd/noe
work=$(mktemp -d /tmp/noe-process-mcp.XXXXXX)
cp scripts/spikes/process-io-mcp/package.json "$work/"
cp scripts/spikes/process-io-mcp/mcp-client-entry.js "$work/"
(cd "$work" && npm install --ignore-scripts --no-audit --no-fund)
"$work/node_modules/.bin/esbuild" "$work/mcp-client-entry.js" \
  --bundle --format=esm --platform=neutral --main-fields=module,main \
  '--external:noe:*' '--external:node:*' \
  --outfile="$work/mcp-client.bundle.mjs"
```

The result is a reusable module with an ESM `Client` export. Node/npm/esbuild are needed only to prepare that file. NOE runs the bundle and transport directly.

## Import-map consumer

Use an exact-bare import map with absolute targets. Map targets are resolved relative to the map file when written relatively. A consumer can configure process launch explicitly:

```json
{
  "imports": {
    "mcp/client": "/absolute/path/mcp-client.bundle.mjs",
    "mcp/transport": "/absolute/path/noe-transport.js"
  }
}
```

```js
import { Client } from "mcp/client";
import { NoeTransport } from "mcp/transport";

const transport = new NoeTransport({
  command: "/absolute/path/noe",
  args: ["run", "/absolute/path/mcp-server.js"],
  cwd: "/absolute/path/server-working-directory",
});
const client = new Client({ name: "example", version: "1" });
try {
  await client.connect(transport);
  console.log((await client.listTools()).tools);
} finally {
  await client.close();
}
```

`consumer/` uses a shared import-mapped exercise from a main script, a module Worker, and fixture-provider `run_js`. The local `server.js` requires the SDK's `notifications/initialized` notification before listing its echo tool. For three concurrent calls it sends responses in reverse order, checks request-ID correlation, and echoes a 270000-byte UTF-8 string. Failure modes also cover executable startup failure, unexpected stdout EOF, malformed JSON, pending-RPC rejection, child cleanup, and close racing with startup. To verify forced cleanup while an input write is blocked, the fixture sends an explicit JSON-RPC readiness notification and then stops reading stdin; the consumer queues sixteen 512 KiB writes before closing, so the check is gated by readiness and confirms that writes remain pending before close.

## Run the opt-in E2E

The Go E2E skips when no prepared SDK bundle is supplied. It does not install npm packages or invoke Node:

```sh
NOE_MCP_SDK_BUNDLE="$work/mcp-client.bundle.mjs" \
  go test -tags=e2e ./tests -run '^TestImportMapMCPProcessIO$' -count=1
```

On Linux, with SDK 1.32.1 and esbuild 0.25.12, this test passed for main, Worker, and fixture-agent `run_js`, as well as the startup-failure, EOF, malformed-message, and close/child-reclamation paths. It uses only the local fixture server and a local provider fixture; no real provider, credential, or paid API is involved.
