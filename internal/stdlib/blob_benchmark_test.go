package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func BenchmarkBlobCopy(b *testing.B) {
	for _, operation := range []string{"construct", "slice", "form"} {
		b.Run(operation, func(b *testing.B) {
			rt := quickjs.New()
			defer rt.Close()
			if err := stdlib.WebAPIs(rt, nil); err != nil {
				b.Fatal(err)
			}
			fn, err := rt.Eval(`(function () {
const blob = new Blob([new Uint8Array(1 << 20)]);
return function(operation) {
  if (operation === "construct") return new Blob([blob]).size;
  if (operation === "slice") return blob.slice(1, blob.size - 1).size;
  const form = new FormData(); form.append("file", blob, "data.bin"); return form.get("file").size;
};
})()`)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := fn.Call(operation); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
