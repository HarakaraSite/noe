package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryFileAPIs(t *testing.T) {
	dir := t.TempDir()
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(dir, "every-byte.bin"), allBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.bin"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	japanesePath := filepath.Join(dir, "日本語.bin")
	if err := os.WriteFile(japanesePath, []byte{0, 255, 128}, 0600); err != nil {
		t.Fatal(err)
	}

	s, err := NewSession(Options{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	evalBinaryFile(t, s, `await noe.readFile("every-byte.bin").then(data => data.length === 256 && data.every((value, index) => value === index))`, "true")
	evalBinaryFile(t, s, `await noe.readFile("empty.bin").then(data => data instanceof Uint8Array && data.length === 0)`, "true")
	evalBinaryFile(t, s, `await noe.readFile("日本語.bin").then(data => data.length === 3 && data[0] === 0 && data[1] === 255 && data[2] === 128)`, "true")
	evalBinaryFile(t, s, `await noe.readFile(`+jsString(t, japanesePath)+`).then(data => data.length === 3 && data[0] === 0 && data[1] === 255 && data[2] === 128)`, "true")
	evalBinaryFile(t, s, `await (async () => {
		const first = await noe.readFile("every-byte.bin");
		const second = await noe.readFile("every-byte.bin");
		first[0] = 99;
		return second[0] === 0 && second[255] === 255;
	})()`, "true")

	evalBinaryFile(t, s, `await noe.writeFile("written-all.bin", Uint8Array.from({length: 256}, (_, index) => index)) === undefined`, "true")
	if got := readBinaryFile(t, filepath.Join(dir, "written-all.bin")); !bytes.Equal(got, allBytes) {
		t.Fatalf("writeFile changed bytes: got %v, want 0..255", got)
	}

	evalBinaryFile(t, s, `await (async () => {
		const source = new Uint8Array([1, 2, 3, 4]);
		const pending = noe.writeFile("snapshot.bin", source);
		source.fill(9);
		source.buffer.transfer();
		await pending;
	})()`, "")
	if got, want := readBinaryFile(t, filepath.Join(dir, "snapshot.bin")), []byte{1, 2, 3, 4}; !bytes.Equal(got, want) {
		t.Fatalf("snapshot after mutation and detach = %v, want %v", got, want)
	}

	evalBinaryFile(t, s, `await (async () => {
		const source = new Uint8Array([99, 10, 0, 255, 88]);
		await noe.writeFile(`+jsString(t, japanesePath)+`, source.subarray(1, 4));
	})()`, "")
	if got, want := readBinaryFile(t, japanesePath), []byte{10, 0, 255}; !bytes.Equal(got, want) {
		t.Fatalf("subarray write = %v, want %v", got, want)
	}
	evalBinaryFile(t, s, `await (async () => {
		class DerivedBytes extends Uint8Array {}
		await noe.writeFile("derived.bin", new DerivedBytes([12, 34]));
	})()`, "")
	if got, want := readBinaryFile(t, filepath.Join(dir, "derived.bin")), []byte{12, 34}; !bytes.Equal(got, want) {
		t.Fatalf("derived Uint8Array write = %v, want %v", got, want)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty-output.bin"), []byte{9}, 0600); err != nil {
		t.Fatal(err)
	}
	evalBinaryFile(t, s, `await noe.writeFile("empty-output.bin", new Uint8Array(0))`, "")
	if got := readBinaryFile(t, filepath.Join(dir, "empty-output.bin")); len(got) != 0 {
		t.Fatalf("empty Uint8Array write left bytes %v", got)
	}

	protectedPath := filepath.Join(dir, "protected.bin")
	protected := []byte("keep existing contents")
	if err := os.WriteFile(protectedPath, protected, 0600); err != nil {
		t.Fatal(err)
	}
	evalBinaryFile(t, s, `await (async () => {
	const values = [
		"text",
		[1, 2],
		new ArrayBuffer(2),
		new DataView(new ArrayBuffer(2)),
		new Uint16Array([1]),
		new Proxy(new Uint8Array([1]), {}),
		{[Symbol.toStringTag]: "Uint8Array"},
		(() => { const value = new Uint16Array([1]); Object.setPrototypeOf(value, Uint8Array.prototype); return value; })(),
		(() => { const value = new Uint8Array([1]); value.buffer.transfer(); return value; })(),
	];
	for (const value of values) {
		let rejected = false;
		try { await noe.writeFile("protected.bin", value); }
		catch (error) { rejected = error instanceof TypeError; }
		if (!rejected) throw new Error("writeFile accepted an invalid byte source");
	}
	})()`, "")
	if got := readBinaryFile(t, protectedPath); !bytes.Equal(got, protected) {
		t.Fatalf("rejected write changed existing file to %q", got)
	}
	evalBinaryFile(t, s, `await (async () => {
	let rejected = false;
		try { await noe.writeFile("must-not-exist.bin", {}); }
		catch (error) { rejected = error instanceof TypeError; }
		return rejected;
	})()`, "true")
	if _, err := os.Stat(filepath.Join(dir, "must-not-exist.bin")); !os.IsNotExist(err) {
		t.Fatalf("invalid byte source created a file: %v", err)
	}

	for _, source := range []string{
		`await noe.readFile("missing.bin")`,
		`await noe.writeFile("missing-dir/output.bin", new Uint8Array([1]))`,
	} {
		result := s.Eval(context.Background(), source)
		if result.Exit || result.Err == nil {
			t.Fatalf("input %q result = %+v, want a recoverable rejected Promise", source, result)
		}
		continued := s.Eval(context.Background(), `40 + 2`)
		if continued.Exit || continued.Err != nil || continued.Output != "42" {
			t.Fatalf("session did not recover after %q: %+v", source, continued)
		}
	}
}

func TestUnawaitedBinaryWriteCompletesBeforeExecuteReturns(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "write.js")
	code, err := Execute(context.Background(), Source{
		Name: name,
		Code: `noe.writeFile("unawaited.bin", new Uint8Array([0, 255, 1]));`,
	}, Options{Cwd: dir})
	if code != 0 || err != nil {
		t.Fatalf("Execute code=%d err=%v", code, err)
	}
	if got, want := readBinaryFile(t, filepath.Join(dir, "unawaited.bin")), []byte{0, 255, 1}; !bytes.Equal(got, want) {
		t.Fatalf("unawaited write = %v, want %v", got, want)
	}
}

func evalBinaryFile(t *testing.T, s *Session, source, want string) {
	t.Helper()
	result := s.Eval(context.Background(), source)
	if result.Exit || result.Err != nil || result.Output != want {
		t.Fatalf("input %q result = %+v, want output %q", source, result, want)
	}
}

func readBinaryFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
