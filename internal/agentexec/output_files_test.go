package agentexec

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestOutputSpillPreservesAllBytesAtFirstOverflow(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	directory, err := NewOutputDirectory("/logs", stdlib.Profile{
		Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := &capture{
		capturedOutput: capturedOutput{maxBytes: 9},
		spill:          &outputSpill{directory: directory, stream: "stdout"},
	}
	input := [][]byte{[]byte("HEADabc"), {0xff, 0xc3}, {0xa9, 'x'}, []byte("dropped-middle-TAIL")}
	for _, part := range input[:2] {
		if _, err := c.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "logs"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("files at exact capture limit: %v, %v", entries, err)
	}
	for _, part := range input[2:] {
		if _, err := c.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	var result core.Result
	preview, omitted, file := c.finish(&result, "stdout")
	if file == nil || len(result.Diagnostics) != 0 || omitted <= 0 || !strings.HasPrefix(preview, "HEAD") || !strings.HasSuffix(preview, "TAIL") {
		t.Fatalf("preview=%q omitted=%d file=%+v diagnostics=%v", preview, omitted, file, result.Diagnostics)
	}
	want := bytes.Join(input, nil)
	got, err := os.ReadFile(filepath.Join(root, file.Path))
	if err != nil || !bytes.Equal(got, want) || file.Bytes != int64(len(want)) {
		t.Fatalf("saved=%q want=%q file=%+v err=%v", got, want, file, err)
	}
}

func TestOutputSpillUnavailableDirectoryKeepsPreview(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	directory, err := NewOutputDirectory(dir, stdlib.Profile{Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	c := &capture{
		capturedOutput: capturedOutput{maxBytes: 8},
		spill:          &outputSpill{directory: directory, stream: "stderr"},
	}
	data := []byte("HEADmiddleTAIL")
	if n, err := c.Write(data); n != len(data) || err != nil {
		t.Fatalf("capture write = %d, %v", n, err)
	}
	result := core.Result{Reason: "completed"}
	preview, omitted, file := c.finish(&result, "stderr")
	if file != nil || omitted != 6 || !strings.HasSuffix(preview, "TAIL") || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "output_save_error" || result.Reason != "completed" {
		t.Fatalf("preview=%q omitted=%d file=%+v result=%+v", preview, omitted, file, result)
	}
}
