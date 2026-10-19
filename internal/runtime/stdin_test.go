package runtime

import (
	"io"
	"strings"
	"testing"
)

func TestProcessStdinReadIsSynchronousAndConsumesInput(t *testing.T) {
	for _, input := range []string{"", "[100,200,300]\n", "雪\n日本語\n", "last line"} {
		t.Run(input, func(t *testing.T) {
			reader := &trackedStdin{Reader: strings.NewReader(input)}
			want := jsString(t, input)
			source := `const first = process.stdin.read();
if (first !== ` + want + ` && !(first === null && ` + want + ` === "")) throw Error("stdin contents");
if (first instanceof Promise) throw Error("stdin read must be synchronous");
if (process.stdin.read() !== null) throw Error("stdin was not consumed");
console.log("stdin ready");`
			out := runStdlib(t, source, Options{Stdin: reader})
			if out != "stdin ready\n" {
				t.Fatalf("output=%q", out)
			}
			if reader.closed {
				t.Fatal("runtime closed borrowed stdin")
			}
		})
	}
	if out := runStdlib(t, `console.log(process.stdin.read() === null)`, Options{}); out != "true\n" {
		t.Fatalf("empty default stdin output=%q", out)
	}
}

type trackedStdin struct {
	io.Reader
	closed bool
	reads  int
}

func (r *trackedStdin) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

func (r *trackedStdin) Close() error {
	r.closed = true
	return nil
}
