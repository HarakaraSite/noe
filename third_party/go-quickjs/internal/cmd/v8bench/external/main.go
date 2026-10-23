// Command external runs the V8 version 7 benchmark suite on a JavaScript
// engine that is a program of its own -- QuickJS's qjs, or node -- the same
// way internal/cmd/v8bench runs it on go-quickjs, for comparing them; see
// package v8bench.
//
//	go run ./internal/cmd/v8bench/external -engine qjs -cmd /path/to/qjs -dir /tmp/v8-v7
package main

import "forge.harakara.site/littleisland/noe/third_party/go-quickjs/internal/v8bench"

func main() { v8bench.MainExternal() }
