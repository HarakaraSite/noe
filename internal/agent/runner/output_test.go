package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapturedOutputRetainsHeadTailAcrossWrites(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, size := range []int{0, 1, capturedOutputLimit / 2, capturedOutputLimit - 1, capturedOutputLimit, capturedOutputLimit + 1, 3 * capturedOutputLimit} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte('a' + rng.IntN(26))
		}
		for _, chunk := range []int{1, 4093, capturedOutputLimit / 2, size + 1} {
			t.Run(fmt.Sprintf("bytes=%d/chunk=%d", size, chunk), func(t *testing.T) {
				var capture capturedOutput
				for offset := 0; offset < len(data); offset += chunk {
					p := data[offset:min(offset+chunk, len(data))]
					if n, err := capture.Write(p); n != len(p) || err != nil {
						t.Fatalf("Write: n=%d err=%v", n, err)
					}
				}
				got, omitted := capture.result()
				if size <= capturedOutputLimit {
					if got != string(data) || omitted != 0 {
						t.Fatalf("short output changed: bytes=%d omitted=%d", len(got), omitted)
					}
					return
				}
				wantOmitted := int64(size - capturedOutputLimit)
				marker := fmt.Sprintf("\n[NOE: output truncated; %d bytes omitted]\n", wantOmitted)
				want := string(data[:capturedOutputLimit/2]) + marker + string(data[size-capturedOutputLimit/2:])
				if got != want || omitted != wantOmitted {
					t.Fatalf("wrong retained sections: bytes=%d omitted=%d want=%d", len(got), omitted, wantOmitted)
				}
			})
		}
	}
}

func TestCapturedOutputPreservesUTF8AcrossTruncation(t *testing.T) {
	data := []byte(strings.Repeat("あ🙂", capturedOutputLimit))
	var capture capturedOutput
	// Deliberately split inside multibyte characters as well as the ring boundary.
	for offset := 0; offset < len(data); offset += 5 {
		_, _ = capture.Write(data[offset:min(offset+5, len(data))])
	}
	got, omitted := capture.result()
	if !utf8.ValidString(got) || omitted <= 0 {
		t.Fatalf("invalid truncated UTF-8 or missing omission: omitted=%d", omitted)
	}
	marker := fmt.Sprintf("\n[NOE: output truncated; %d bytes omitted]\n", omitted)
	parts := strings.Split(got, marker)
	if len(parts) != 2 || !bytes.HasPrefix(data, []byte(parts[0])) || !bytes.HasSuffix(data, []byte(parts[1])) {
		t.Fatal("retained Unicode sections differ from original bytes")
	}
	if int64(len(data)-len(parts[0])-len(parts[1])) != omitted {
		t.Fatal("omission count does not include removed partial characters")
	}
}

func TestCapturedOutputMemoryDoesNotGrowWithTotalOutput(t *testing.T) {
	var capture capturedOutput
	chunk := bytes.Repeat([]byte("x"), capturedOutputLimit)
	for range 1024 {
		if n, err := capture.Write(chunk); n != len(chunk) || err != nil {
			t.Fatalf("Write: n=%d err=%v", n, err)
		}
	}
	if capacity := cap(capture.head) + cap(capture.tail); capacity > capturedOutputLimit {
		t.Fatalf("retained allocation grew to %d bytes", capacity)
	}
	_, omitted := capture.result()
	if want := int64(len(chunk)*1024 - capturedOutputLimit); omitted != want {
		t.Fatalf("omitted=%d want=%d", omitted, want)
	}
}

func TestRunnerResultOmitsTruncationFieldsForShortOutput(t *testing.T) {
	encoded, err := json.Marshal(Result{Stdout: "short", ExitCode: 0, Reason: "completed"})
	if err != nil || strings.Contains(string(encoded), "OmittedBytes") {
		t.Fatalf("short result JSON=%s err=%v", encoded, err)
	}
}
