# node:path

Computes path strings without reading the filesystem. NOE's bare path module uses POSIX rules on every host; path/posix and path/win32 select a flavor explicitly, and each path object exposes both posix and win32 properties.

### Exports

`_makeLong`, `basename`, `default`, `delimiter`, `dirname`, `extname`, `format`, `isAbsolute`, `join`, `normalize`, `parse`, `posix`, `relative`, `resolve`, `sep`, `toNamespacedPath`, `win32`

resolve and relative use process.cwd() when available. This module transforms path text only; node:fs and noe:fs-handle perform file I/O.

## join

Syntax: join(...parts) -> string. It joins path segments with the selected flavor's separator and normalizes the result.

~~~js
import path from "node:path";

const file = path.join("reports", "2026", "summary.txt");
~~~

Use node:path/win32 or node:path/posix when a path syntax must remain stable across hosts.

## resolve

Syntax: resolve(...parts) -> absolutePath. It works from right to left until it finds an absolute segment, then normalizes. If none is absolute, it uses process.cwd() in NOE.

~~~js
import { resolve } from "node:path";

const absolute = resolve("data", "input.json");
~~~

## dirname

Syntax: dirname(path) -> string. It returns the directory part using the selected flavor's separator rules.

~~~js
import { dirname } from "node:path";

console.log(dirname("/work/report.txt")); // /work
~~~

## basename

Syntax: basename(path, suffix?) -> string. It returns the final path component and removes the optional suffix only when it matches the end.

~~~js
import { basename } from "node:path";

console.log(basename("/work/report.txt", ".txt")); // report
~~~

## extname

Syntax: extname(path) -> string. It returns the final component's extension, including its leading dot, or an empty string if there is no extension.

~~~js
import { extname } from "node:path";

console.log(extname("report.tar.gz")); // .gz
~~~

## posix

The posix property exposes the POSIX flavor. node:path/posix also selects this flavor as its default export. Import it when POSIX separators and roots must remain stable across hosts.

~~~js
import path from "node:path/posix";

console.log(path.join("one", "two"));
~~~

## win32

The win32 property exposes the Windows flavor. node:path/win32 selects it as its default export. Import it to interpret Windows path strings on any host. In NOE, bare node:path remains POSIX even on Windows.

~~~js
import path from "node:path/win32";

console.log(path.join("C:\\", "work", "report.txt"));
~~~

### Source basis

Pinned implementation: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
