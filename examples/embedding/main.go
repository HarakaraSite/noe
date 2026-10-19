// This example embeds NOE through its public Go package.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	rt := quickjs.New()
	defer rt.Close()
	loop := stdlib.NewLoop(rt)
	defer loop.Close()
	_, err = stdlib.Install(rt, stdlib.Config{
		Core: stdlib.CoreConfig{
			Loop: loop, Stdout: os.Stdout, Stderr: os.Stderr,
			// The IO group requires process for spawn's environment and cwd.
			// No host environment or process-exit callback is granted here.
			Process: &stdlib.Process{Cwd: cwd},
		},
		IO:   &stdlib.IOConfig{Stdin: strings.NewReader("Hello from the embedded runtime.\n"), Cwd: cwd},
		Docs: true,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rt.AbortOn(ctx.Done())
	result, err := rt.EvalContext(ctx, `(async () => {
		const {docs} = await import("noe:docs");
		const {stdin, stdout, pipe} = await import("noe:io");
		console.log("Reference:", docs("noe:io").split("\n")[0]);
		await pipe(stdin, stdout, {preventClose: true});
	})()`)
	if err != nil {
		return err
	}
	if err := loop.Run(ctx); err != nil {
		return err
	}
	_, err = result.Await(ctx)
	return err
}
