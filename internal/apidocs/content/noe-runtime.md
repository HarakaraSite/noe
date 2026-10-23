# `noe:runtime`

The managed runtime exposes non-evaluating module inspection. Normal execution
and inspection use the same parser, static import resolver, and linker.

## check

Signature: `check(code: string) -> {ok: boolean, stage: string, entry: string, diagnostics?: object[]}`

```js
import {check} from "noe:runtime";
export default check('import {readFile} from "node:fs/promises"; export default 42;');
```

The synchronous function creates a fresh inspection VM using the current
capabilities, loader factory, and explicitly configured inspection initializer.
It does not execute entry or dependency module bodies. It returns `ok: false`
with structured diagnostics for invalid code; it does not throw merely because
the inspected code fails to parse or link. Cancellation follows the current
execution context.

A successful `stage: "link"` means the static graph linked. Dynamic imports,
runtime values, and effects after evaluation are not checked. Separate check
and execute calls can read different file versions. Loader and inspection
initializer Go callbacks may have side effects; ordinary runtime initializers
are not replayed. Custom modules installed only by a normal initializer are not
automatically available to inspection. This is inspection, not a sandbox for
arbitrary host callbacks.
