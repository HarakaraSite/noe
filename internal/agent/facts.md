An agent task may use the `run_js` tool to execute an ES module with top-level await. Each invocation runs in a fresh owned JavaScript runtime with a new global scope and module cache. Relative imports from the virtual entry resolve from the execution's configured working directory; file-backed modules resolve relative imports from their importing module.

Available runtime APIs depend on the selected profile. The `noe:docs` module describes the full API catalog, including APIs that are not enabled in a particular profile.

The `run_js` result reports stdout, stderr, exit code, and execution reason. Captured stdout and stderr each retain the first and last 32 KiB and report omitted bytes when truncated. `NOE_CONTEXT_STATUS` is a user message in conversation history.

The `noe:agent` module exposes `run_agent`, `spawn_agent`, `observe_agent`, `queue_agent`, `wait_task`, `read_task_result`, `steer_agent`, `cancel_task`, `stop_agent`, and `list_models`. Agent and task operations are checked against the current execution's ownership scope. `spawn_agent` and `queue_agent` return accepted command identifiers; `wait_task` observes one command's retained result. `cancel_task` cancels one command, while `stop_agent` stops an instance and its remaining work.
