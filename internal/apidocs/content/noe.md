# NOE

NOE (No Ordinary Engine) is a JavaScript runtime and managed agent execution environment written in Go. It uses go-quickjs, a pure-Go implementation based on QuickJS-NG, and embeds the API reference for the runtime version in this binary.

NOE uses the pinned go-quickjs ECMAScript engine and supplies supported Web and Node-style APIs through its own maintained standard library, based on go-quickjs v0.27.0. These APIs follow the NOE-maintained implementation; NOE does not promise compatibility with all of Node.js. Node-style built-ins can be imported by their supported bare name or `node:` name. HTTP/HTTPS module imports are enabled by the CLI and selected explicitly by Go hosts; imports support JavaScript and explicit JSON/text/bytes types. npm installation and implicit package discovery are unavailable.

NOE also provides modules for agents, model requests, saved-event analysis, decisions, images, authentication, child processes, process metadata, standard I/O, HTTP, sockets, and signals. These process APIs are exposed through `noe:process`. File handles are available through the standard `noe:fs` API. The managed-agent `run_js` tool executes code in a fresh, owned NOE runtime for each call. NOE manages agent and tool lifecycles and resources created by an evaluation; model inference is performed by the selected provider.

The ordinary top-level runtime and Workers expose different NOE extensions. `noe:sockets`, `noe:signals`, `noe:http`, and `noe:docs` are available in Workers. Host standard I/O is exposed by the selected `noe:process` capabilities. `noe:agent`, `noe:model`, `noe:agent-log`, `noe:decision`, `noe:image`, and `noe:auth` are main-runtime APIs when selected by the host. The catalog is a full reference and does not indicate which optional modules a particular runtime installed. Standard-library APIs, including `noe:process` and `noe:fs`, have their own Worker behavior and limits.

Scripts run with the operating-system permissions of the NOE process. NOE is not a sandbox. A module import does not grant a separate permission boundary. Handles, sockets, child processes, signal subscriptions, and agent work are owned by their creating runtime or managed task and are recovered when that owner ends.

Use `noe docs` for the topic index, `noe docs <topic>` for a topic overview and its member IDs, and `noe docs <topic> <member>` for one member's reference. From JavaScript, import the synchronous `docs` function from `noe:docs`.

Each module overview identifies its public exports, aliases, owner, type-declaration file, and host requirements. noestdlib owns `stdlib/types/noestdlib.d.ts`; noeagentlib owns `agent/types/noeagentlib.d.ts`, including agent/model/image/decision/auth/log modules. Obtain these separate files from the corresponding source version; the binary does not download or combine them. Reading this catalog never evaluates the described module. Type declarations and reference entries do not grant or detect runtime capabilities.

Use the standard global `fetch()` for HTTP requests. Import host-specific listener and WebSocket helpers from `noe:http`; its selected exports and availability are described in `docs("noe:http")`, including demand-driven request-body reads, body limits, `Expect: 100-continue`, and resource-lifetime details.

Web Storage globals are available in current unreleased source after v2.18.0.
`sessionStorage` is runtime-local. CLI `run` and `eval` accept
`--local-storage path` before the script/code operand to select a persistent
`localStorage` file; the containing directory must exist. Go hosts select
`Modules.LocalStorage` with `Modules.FS`. See `docs("globals", "Storage")`.
