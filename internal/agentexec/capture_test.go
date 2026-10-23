package agentexec

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCaptureSpillsFullOutputAndTransfersOwnership(t *testing.T) {
	body := []byte(strings.Repeat("界\"\\\n", 40000))
	c := &capture{retainFull: true}
	for start := 0; start < len(body); start += 1001 {
		end := min(start+1001, len(body))
		if _, err := c.Write(body[start:end]); err != nil {
			t.Fatal(err)
		}
	}
	preview, omitted := c.result()
	if omitted == 0 || !utf8.ValidString(preview) || !strings.Contains(preview, "output truncated") {
		t.Fatalf("invalid preview omitted=%d", omitted)
	}
	reader, err := c.takeFull()
	if err != nil || reader == nil {
		t.Fatalf("full reader=%v err=%v", reader, err)
	}
	name := reader.(*capturedFile).Name()
	c.close() // Transfer means the original capture no longer owns this file.
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("full output changed, len=%d error=%v", len(got), err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("transferred temporary output remains: %v", err)
	}
}

func TestCaptureCleansUntransferredSpill(t *testing.T) {
	c := &capture{retainFull: true}
	if _, err := c.Write([]byte(strings.Repeat("x", capturedOutputLimit+1))); err != nil {
		t.Fatal(err)
	}
	name := c.file.Name()
	c.close()
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("untransferred output remains: %v", err)
	}
}
