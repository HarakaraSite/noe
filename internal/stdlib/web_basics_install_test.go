package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestWebBasicsWithoutLoop(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	if err := stdlib.WebAPIs(rt, nil); err != nil {
		t.Fatal(err)
	}
	_, err := rt.Eval(`
const output = new Uint8Array(1);
if(new TextEncoder().encodeInto("あ",output).written!==0)throw Error("encoding without Loop");
const target=new EventTarget();let value;
target.addEventListener("test",event=>{value=event.detail;});
target.dispatchEvent(new CustomEvent("test",{detail:42}));
if(value!==42)throw Error("CustomEvent without Loop");
const decoder=new TextDecoder();decoder.decode(new Uint8Array([239]),{stream:true});
if(decoder.decode(new Uint8Array([187,191,226,130]))!=="�")throw Error("decoder without Loop");
const controller=new AbortController();
target.addEventListener("bound",()=>{throw Error("bound listener without Loop");},{signal:controller.signal});
controller.abort();target.dispatchEvent(new Event("bound"));
const detail={value:1};const mark=performance.mark("start",{startTime:2,detail});detail.value=9;
const measured=performance.measure("work",{start:"start",end:5});
if(mark.detail.value!==1||measured.duration!==3||performance.getEntries().length!==2)throw Error("User Timing without Loop");
`)
	if err != nil {
		t.Fatal(err)
	}
}
