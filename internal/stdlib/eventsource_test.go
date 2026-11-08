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
	if _, err := rt.Eval(`let missingURL;try{new Request("https://example.test/");}catch(error){missingURL=error.name;}if(missingURL!=="TypeError")throw Error("missing URL backend was not explicit");`); err != nil {
		t.Fatal(err)
	}
	if err := stdlib.WebAPIs(rt, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`if(new Request("https://EXAMPLE.test:00443/☃").url!=="https://example.test/%E2%98%83")throw Error("late Web URL installation failed");`); err != nil {
		t.Fatal(err)
	}
}
