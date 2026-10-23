package noestdlib

import (
	"bytes"
	"io"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// The struct type is comparable, but its interface can contain a slice or map.
type interfaceOutputWriter struct{ io.Writer }

type mapOutputWriter map[string]int

func (w mapOutputWriter) Write(p []byte) (int, error) {
	w["bytes"] += len(p)
	return len(p), nil
}

func TestOutputNormalizationChecksDynamicComparability(t *testing.T) {
	slice := interfaceOutputWriter{uncomparableWriter{}}
	mapping := interfaceOutputWriter{mapOutputWriter{}}
	nested := interfaceOutputWriter{mapping}
	cases := []struct {
		name           string
		stdout, stderr io.Writer
	}{
		{"slice in interface", slice, slice},
		{"map in interface", mapping, mapping},
		{"nested interface", nested, nested},
		{"comparable left value", interfaceOutputWriter{&bytes.Buffer{}}, mapping},
		{"comparable right value", mapping, interfaceOutputWriter{&bytes.Buffer{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := quickjs.New()
			defer rt.Close()
			_, err := InstallProfile(rt, ProfileConfig{Core: stdlib.Config{
				Stdout: tc.stdout, Stderr: tc.stderr,
				Process: &stdlib.Process{Stdout: tc.stdout, Stderr: tc.stderr},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := rt.Eval(`console.log("console"); console.log("out"); console.error("err");`); err != nil {
				t.Fatal(err)
			}
		})
	}
}
