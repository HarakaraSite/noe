package runtime

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStdlibBinaryFilesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.bin")
	output := filepath.Join(dir, "output.bin")
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	if err := os.WriteFile(input, allBytes, 0600); err != nil {
		t.Fatal(err)
	}

	source := `import {readFile, writeFile} from "node:fs/promises";
const first = await readFile(` + jsString(t, input) + `);
const second = await readFile(` + jsString(t, input) + `);
if (!(first instanceof Uint8Array) || first.length !== 256 || !first.every((value, index) => value === index)) throw Error("read bytes");
first[0] = 99;
if (second[0] !== 0 || second[255] !== 255) throw Error("read buffers shared storage");
await writeFile(` + jsString(t, output) + `, second.subarray(1, 4));
console.log("binary files ready");`
	if out := runStdlib(t, source, Options{}); out != "binary files ready\n" {
		t.Fatalf("output=%q", out)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{1, 2, 3}; !bytes.Equal(got, want) {
		t.Fatalf("written bytes=%v, want %v", got, want)
	}
}
