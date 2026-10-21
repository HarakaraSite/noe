package stdlib

import "forge.harakara.site/littleisland/noe/v2/internal/process"

// DispatchProcessHelper handles a reserved process-helper invocation. A helper
// main calls this with os.Args[1:] before normal application work and exits with
// the returned code when handled. A dedicated helper avoids running application
// initialization twice; a self-helper requires all dependency init functions to
// be safe to run again. Windows uses Job Objects and needs no helper.
func DispatchProcessHelper(args []string) (code int, handled bool) {
	return process.DispatchHelper(args)
}
