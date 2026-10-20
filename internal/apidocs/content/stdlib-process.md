# node:process

Exports the process object as the module default and as the global process. It reports NOE's arguments, environment, working directory, standard streams, and process controls; it is not a full Node process object.

### Exports

`default`

Use the global process object or import process from "node:process". There are no named exports: importing {cwd}, {env}, or {argv} is unsupported. Call process.cwd() and read process.env/process.argv instead.

In NOE, process.argv contains the executable, current source name, then script arguments. process.env is a JavaScript snapshot; editing it does not change the host environment. process.exit() ends the current NOE run.

## cwd

Syntax: process.cwd() -> string. It returns the working directory captured for this NOE run.

~~~js
import process from "node:process";

console.log(process.cwd());
~~~

## env

process.env is an object containing the environment variables present when NOE created the runtime. It can be read or changed as a JavaScript object, but changing it does not mutate the parent shell's environment.

~~~js
import process from "node:process";

console.log(process.env.HOME ?? "(unset)");
~~~

NOE passes its configured environment to child programs; set a child call's env option when that call needs a different environment.

## argv

process.argv has the shape [executable, source, ...scriptArgs]. For noe run script.js one, the source is script.js and the remaining entry is one.

~~~js
import process from "node:process";

console.log(process.argv.slice(2));
~~~

## exit

Syntax: process.exit(code = 0). It requests termination of the current NOE runtime with the numeric exit code. In the main runtime NOE records the requested code and closes its runtime; inside a Worker it ends that Worker.

~~~js
import process from "node:process";

if (!process.env.API_TOKEN) process.exit(2);
~~~

This ends the run and its owned runtime resources. Set process.exitCode when the script should finish naturally with a non-zero code.

### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
