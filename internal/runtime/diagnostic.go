package runtime

import "forge.harakara.site/littleisland/noe/v2/stdlib"

// Diagnostic includes detached JavaScript stacks and command-error details.
func Diagnostic(err error) string { return stdlib.Diagnostic(err) }
