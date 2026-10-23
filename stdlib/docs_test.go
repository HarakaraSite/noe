package stdlib_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestDocsReferenceDoesNotLoadOptionalAPIs(t *testing.T) {
	loads := 0
	profile := stdlib.Profile{NewLoader: func(context.Context, stdlib.Origin) (quickjs.ModuleLoader, error) {
		return func(specifier, referrer string) (string, string, error) {
			loads++
			return "", "", errors.New("target modules must not be loaded by docs")
		}, nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[docs without capabilities]", Module: true, Virtual: true, Code: `
import {docs} from "noe:docs";
const agent = docs("noe:agent");
if (!agent.includes("agent/types/noeagentlib.d.ts") || !agent.includes("Requires an agent")) throw Error("missing agent contract");
const fs = docs("fs");
if (fs !== docs("node:fs") || !fs.includes("stdlib/types/noestdlib.d.ts") || !fs.includes("Profile.Modules.FS")) throw Error("missing filesystem contract");
if (!docs("noe:http").includes("Only the selected functions are exported")) throw Error("conditional exports lost");
if (!docs("noe:model").includes("generate_image")) throw Error("model re-export lost");
`}, profile)
	if err != nil || result.Code != 0 {
		t.Fatalf("reference-only profile: code=%d error=%s", result.Code, stdlib.Diagnostic(err))
	}
	if loads != 0 {
		t.Fatalf("reference evaluated %d target modules", loads)
	}
}
