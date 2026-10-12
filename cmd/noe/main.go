package main

import (
	"context"
	"os"
	"os/signal"

	"noe/internal/cli"
)

func main() {
	ctx := context.Background()
	stop := func() {}
	// REPL owns SIGINT so a terminal evaluation can be interrupted without
	// cancelling the entire session. Other commands keep process cancellation.
	if len(os.Args) < 2 || os.Args[1] != "repl" {
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt)
	}
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
