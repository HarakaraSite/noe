package main

import (
	"context"
	"os"
	"os/signal"

	"forge.harakara.site/littleisland/noe/v2/internal/cli"
	"forge.harakara.site/littleisland/noe/v2/internal/process"
)

func main() {
	if code, handled := process.DispatchHelper(os.Args[1:]); handled {
		os.Exit(code)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
