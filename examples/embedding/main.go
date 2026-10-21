// This example runs a JavaScript module through the public stdlib Profile API.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	profile := stdlib.Profile{
		Environment: stdlib.Environment{
			Cwd: cwd, Args: []string{"embedding"}, Env: map[string]string{},
			Stdout: os.Stdout, Stderr: os.Stderr,
		},
	}
	result, err := stdlib.Execute(ctx, stdlib.Source{
		Name: "[embedding]", Virtual: true, Module: true,
		Code: `import {docs} from "noe:docs";
console.log(docs("noe:docs").split("\n")[0]);
console.log("2 + 2 =", 2 + 2);`,
	}, profile)
	if err != nil {
		return fmt.Errorf("run JavaScript: %s: %w", stdlib.Diagnostic(err), err)
	}
	if result.Code != 0 {
		return fmt.Errorf("JavaScript exited with status %d", result.Code)
	}
	return nil
}
