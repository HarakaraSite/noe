package loganalysis

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

type record struct {
	event       events.Event
	executionID string
	seq         uint64
	typeName    string
}

func readRecords(ctx context.Context, input io.Reader) ([]record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(contextReader{ctx: ctx, reader: input}, 32*1024)
	records := make([]record, 0)
	lineNumber := 0
	for {
		line, complete, readErr := readCompleteLine(ctx, reader)
		if readErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if readErr != io.EOF {
				return nil, fmt.Errorf("read log: %w", readErr)
			}
			// A writer may be between writes. Ignore only the unterminated tail;
			// the next query will include it once its newline is visible.
			break
		}
		if !complete {
			break
		}
		lineNumber++
		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		// Parse before the next reader operation; a short line may borrow its buffer.
		// JSON decoding owns the values retained in rec.
		rec, parseErr := parseRecord(ctx, line)
		if parseErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("invalid log at complete line %d: %w", lineNumber, parseErr)
		}
		records = append(records, rec)
	}
	return records, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > 32*1024 {
		p = p[:32*1024]
	}
	return r.reader.Read(p)
}

// A complete first-read line borrows reader's buffer until the next reader operation.
// Buffer-spanning lines are accumulated separately.
func readCompleteLine(ctx context.Context, reader *bufio.Reader) ([]byte, bool, error) {
	var line []byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		part, err := reader.ReadSlice('\n')
		switch err {
		case nil:
			if line == nil {
				return part, true, nil
			}
			return append(line, part...), true, nil
		case bufio.ErrBufferFull:
			line = append(line, part...)
			continue
		case io.EOF:
			// Do not parse a record until its terminating newline has arrived.
			return nil, false, nil
		default:
			return nil, false, err
		}
	}
}

func parseRecord(ctx context.Context, line []byte) (record, error) {
	if err := ctx.Err(); err != nil {
		return record{}, err
	}
	decoder := json.NewDecoder(contextReader{ctx: ctx, reader: bytes.NewReader(line)})
	decoder.UseNumber()
	var event events.Event
	if err := decoder.Decode(&event); err != nil {
		return record{}, err
	}
	if err := ctx.Err(); err != nil {
		return record{}, err
	}
	if event == nil {
		return record{}, errors.New("expected a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return record{}, errors.New("multiple JSON values in one line")
		}
		return record{}, err
	}
	if err := ctx.Err(); err != nil {
		return record{}, err
	}
	executionID, ok := event["executionId"].(string)
	if !ok || executionID == "" {
		return record{}, errors.New("missing or invalid executionId")
	}
	typeName, ok := event["type"].(string)
	if !ok || typeName == "" {
		return record{}, errors.New("missing or invalid type")
	}
	seq, ok := uintField(event["seq"])
	if !ok || seq == 0 {
		return record{}, errors.New("missing or invalid seq")
	}
	return record{event: event, executionID: executionID, seq: seq, typeName: typeName}, nil
}
