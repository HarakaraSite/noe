package stdlib

import (
	"context"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestBodyCloneBridgeInstallerOrdersAndLifetime(t *testing.T) {
	for _, networkFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "streams-first", true: "network-first"}[networkFirst], func(t *testing.T) {
			rt := quickjs.New()
			t.Cleanup(func() { rt.Close() })
			install := []func() error{func() error { return Streams(rt) }, func() error { return Network(rt, nil) }}
			if networkFirst {
				install[0], install[1] = install[1], install[0]
			}
			for _, operation := range install {
				if err := operation(); err != nil {
					t.Fatal(err)
				}
			}
			module, err := rt.RequireModule("stream/web", "")
			if err != nil {
				t.Fatal(err)
			}
			namespace, err := module.Get("default")
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"bodyTee", "exports", "teeBody"} {
				value, err := namespace.Get(name)
				if err != nil || !value.IsUndefined() {
					t.Fatalf("private operation exposed as %s: value=%v err=%v", name, value, err)
				}
			}
			value, err := rt.Eval(`(async()=>{
 if(typeof fetch!=="undefined")throw Error("network grant added");
 const original=new Response(new Uint8Array([65,66])),copy=original.clone();
 const chunk=await original.body.getReader().read();chunk.value[0]=90;
 if((await copy.bytes()).join()!=="65,66")throw Error("clone bytes without WebAPIs");
 return "PASS";
})()`)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			value, err = value.Await(ctx)
			if err != nil || value.String() != "PASS" {
				t.Fatalf("value=%v err=%v", value, err)
			}
			if err := Streams(rt); err != nil {
				t.Fatal(err)
			}
			if _, ok := bodyTeeFactories.Load(rt); !ok {
				t.Fatal("factory was not retained after Streams reinstallation")
			}
			rt.Close()
			if _, ok := bodyTeeFactories.Load(rt); ok {
				t.Fatal("factory survived runtime close")
			}
		})
	}
}
