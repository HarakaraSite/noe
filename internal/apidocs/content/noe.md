# NOE

NOE (No Ordinary Engine) is a JavaScript runtime and managed agent execution environment written in Go. It uses go-quickjs, a pure-Go implementation based on QuickJS-NG, and embeds the API reference for the runtime version in this binary.

NOE uses the pinned go-quickjs ECMAScript engine and supplies supported Web and Node-style APIs through its own maintained standard library, based on go-quickjs v0.27.0. These APIs follow the NOE-maintained implementation; NOE does not promise compatibility with all of Node.js. Node-style built-ins can be imported by their supported bare name or `node:` name. npm installation, HTTP module imports, and implicit package discovery are unavailable.

NOE also provides built-in modules for agents, decisions, images, authentication, child processes, host I/O, HTTP, positional file handles, sockets, and signals. The managed-agent `run_js` tool executes code in a new NOE process for each call. NOE manages agent and tool lifecycles and resources created by an evaluation; model inference is performed by the selected provider.

The ordinary top-level runtime and Workers expose different NOE extensions. `noe:process`, `noe:io`, `noe:fs-handle`, `noe:sockets`, `noe:signals`, and `noe:http` are available in Workers. `noe:agent`, `noe:decision`, `noe:image`, and `noe:auth` are main-runtime APIs. Standard-library APIs have their own Worker behavior and limits.

Scripts run with the operating-system permissions of the NOE process. NOE is not a sandbox. A module import does not grant a separate permission boundary. Handles, sockets, child processes, signal subscriptions, and agent work are owned by their creating runtime or managed task and are recovered when that owner ends.

Use `noe docs` for the topic index, `noe docs <topic>` for a topic overview and its member IDs, and `noe docs <topic> <member>` for one member's reference. From JavaScript, import the synchronous `docs` function from `noe:docs`.

For HTTP requests and servers, use `import { fetch, serve } from "noe:http"`. It shares the existing Web types and supports response streaming. Read `docs("noe:http")` for request-buffering and resource-lifetime details.
