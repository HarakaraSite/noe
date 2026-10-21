package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func init() {
	if path := os.Getenv("NOE_TEST_INIT_FILE"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		if _, err = f.WriteString("init\n"); err != nil {
			panic(err)
		}
		if err = f.Close(); err != nil {
			panic(err)
		}
	}
}

func main() {
	if os.Getenv("NOE_TEST_SKIP_DISPATCH") == "1" {
		fmt.Print("application")
		return
	}
	if code, handled := stdlib.DispatchProcessHelper(os.Args[1:]); handled {
		os.Exit(code)
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	defer rt.Close()
	view, err := stdlib.Attach(rt, loop, stdlib.Profile{Modules: stdlib.Modules{Commands: &stdlib.CommandOptions{
		Supervisor: os.Getenv("NOE_TEST_SUPERVISOR"),
		Allow:      func(string, []string) error { return nil }, Timeout: 30 * time.Second,
	}}})
	if err != nil {
		return err
	}
	defer view.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code := `import {execFileSync} from "node:child_process";globalThis.result=execFileSync("/bin/sh",["-c","printf command-ok"]);`
	if state := os.Getenv("NOE_TEST_LEAK_STATE"); state != "" {
		value, _ := json.Marshal(state)
		code = `import {execFileSync} from "node:child_process";execFileSync("/bin/sh",["-c",'echo $$ > "$1"; sleep 300 & echo $! >> "$1"; wait',"sh",` + string(value) + `]);`
	}
	_, err = rt.EvalModuleContext(ctx, "command.mjs", code)
	if err != nil {
		return err
	}
	value, err := rt.Eval("result")
	if err != nil {
		return err
	}
	fmt.Print(value.String())
	return nil
}
