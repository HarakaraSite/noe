# Delegation scope and result handoff

Decide whether to delegate by weighing the benefit of separating work or obtaining independent judgment against the cost of transferring context, reading results, and processing input in each agent. Do not start a new agent merely because the phase or role has changed.

Delegation can also shorten completion time by running independent work concurrently. Start independent tasks in parallel when splitting the work is useful, and keep dependent tasks in order.

- Delegate substantial implementation tasks with clear ownership and deliverables that need little intermediate explanation or coordination. Handle small fixes and tightly coupled work yourself.
- Group review work where an independent check of the implementer's assumptions has value. Do not add a reviewer for every phase or file.
- When fixes or rechecks are likely within the same owning task, retain the agent with keepAlive. Queue fixes to the original implementer and re-review to the original reviewer. Stop agents with no remaining work; use the normal lifetime for one-off tasks. Do not assume a child can be retained beyond its owning task.
- State the objective, ownership and deliverables, modification authority, contracts to preserve, and completion criteria. Refer children to authoritative files for detailed background; do not omit necessary conditions just to shorten the request.
- Keep child replies to conclusions, changed areas, reasons for decisions, and unresolved issues. Save detailed check output or logs to a known file when needed and return its path. The parent reads the parts needed for its decision.
- Prefer one wait_tasks call for the whole delegated set instead of polling or Promise.all of completion-only waits. Use includeAnswer:false when answers are not yet needed. On reason:steer, return/export the result and finish run_js so the parent can consider the guidance; do not immediately re-wait in that call. Use wait_task when completion-only waiting is intentional. Read required original answers with read_task_result; do not start a summarizer agent just to retrieve them.
- Put checks that only execute commands and collect results into a script. Do not start a new agent solely for those operations.
- If a child needs further delegation, the caller decides whether it is necessary and explicitly sets allowDelegation:true at startup. If further delegation is refused, continue within the assigned scope or report the missing capability to the caller.

These are default operating guidelines. Follow the user's explicit task split, independent review, validation scope, authority, and stop conditions.
