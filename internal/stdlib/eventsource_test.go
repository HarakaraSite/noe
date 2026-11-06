package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestEventSourcePreservesLowLevelFetchWithoutWebAPIs(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := stdlib.NewLoop(rt)
	defer loop.Close()
	if err := stdlib.Install(rt, stdlib.Config{Loop: loop, NoWebAPIs: true, Fetch: &stdlib.Fetch{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`if(typeof fetch!=="function"||typeof EventSource!=="undefined")throw Error("low-level Fetch changed");`); err != nil {
		t.Fatal(err)
	}
}
