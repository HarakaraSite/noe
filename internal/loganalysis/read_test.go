package loganalysis

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadCompleteLineBorrowsFirstSliceAndAccumulatesLongLines(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader("short\n"+strings.Repeat("x", 40)+"\n"), 16)
	buffered, err := reader.Peek(len("short\n"))
	if err != nil {
		t.Fatal(err)
	}
	line, complete, err := readCompleteLine(context.Background(), reader)
	if err != nil || !complete || string(line) != "short\n" {
		t.Fatalf("short line = %q, complete=%v, err=%v", line, complete, err)
	}
	if &line[0] != &buffered[0] {
		t.Fatal("first-read line did not borrow the reader buffer")
	}
	line, complete, err = readCompleteLine(context.Background(), reader)
	if err != nil || !complete || string(line) != strings.Repeat("x", 40)+"\n" {
		t.Fatalf("long line = %q, complete=%v, err=%v", line, complete, err)
	}
}

func TestReadRecordsOwnsDecodedValuesAcrossBufferReuse(t *testing.T) {
	first := `{"executionId":"first-exec","seq":1,"type":"agent.message","runId":"first-run","text":"original","nested":{"value":"preserved"}}` + "\r\n"
	longText := strings.Repeat("z", 70000)
	second := `{"executionId":"other-exec","seq":2,"type":"agent.message","runId":"other-run","text":"` + longText + `"}` + "\n"
	// A partial, malformed tail is ignored, even after a buffer-spanning line.
	records, err := readRecords(context.Background(), strings.NewReader(first+second+`{"partial":`))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	firstRecord := records[0]
	nested, ok := firstRecord.event["nested"].(map[string]any)
	if firstRecord.executionID != "first-exec" || firstRecord.typeName != "agent.message" ||
		firstRecord.seq != 1 || firstRecord.event["runId"] != "first-run" ||
		firstRecord.event["text"] != "original" || !ok || nested["value"] != "preserved" {
		t.Fatalf("first record changed after buffer reuse: %+v", firstRecord)
	}
	if records[1].event["text"] != longText {
		t.Fatal("buffer-spanning record text changed")
	}
}

func TestReadRecordsReportsMalformedCompleteLineAfterLongLine(t *testing.T) {
	long := `{"executionId":"exec","seq":1,"type":"agent.message","runId":"run","text":"` + strings.Repeat("x", 70000) + `"}` + "\r\n"
	_, err := readRecords(context.Background(), strings.NewReader(long+"not json\r\n"+`{"unterminated":`))
	if err == nil || !strings.Contains(err.Error(), "invalid log at complete line 2:") {
		t.Fatalf("malformed complete line error = %v", err)
	}
}

type longLineFailureReader struct {
	reads  int
	cancel context.CancelFunc
	err    error
}

func (r *longLineFailureReader) Read(p []byte) (int, error) {
	r.reads++
	if r.reads == 1 {
		for i := range p {
			p[i] = 'x'
		}
		if r.cancel != nil {
			r.cancel()
		}
		return len(p), nil
	}
	return 0, r.err
}

func TestReadRecordsLongLineCancellationAndReaderError(t *testing.T) {
	t.Run("cancellation during accumulation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		input := &longLineFailureReader{cancel: cancel, err: io.EOF}
		_, err := readRecords(ctx, input)
		if !errors.Is(err, context.Canceled) || input.reads != 1 {
			t.Fatalf("err=%v reads=%d, want cancellation after first full buffer", err, input.reads)
		}
	})
	t.Run("reader error during accumulation", func(t *testing.T) {
		failure := errors.New("read failure")
		input := &longLineFailureReader{err: failure}
		_, err := readRecords(context.Background(), input)
		if !errors.Is(err, failure) || !strings.Contains(err.Error(), "read log:") || input.reads != 2 {
			t.Fatalf("err=%v reads=%d, want wrapped reader failure", err, input.reads)
		}
	})
}
