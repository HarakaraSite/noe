package observation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"noe/internal/events"
)

type record struct {
	event       events.Event
	executionID string
	seq         uint64
	typeName    string
}

func readRecords(ctx context.Context, path string) ([]record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open observation log %q: %w", path, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat observation log %q: %w", path, err)
	}
	cutoff := info.Size()
	reader := bufio.NewReaderSize(contextReader{ctx: ctx, reader: io.LimitReader(file, cutoff)}, 32*1024)
	records := make([]record, 0)
	lineNumber := 0
	for {
		line, complete, readErr := readCompleteLine(ctx, reader)
		if readErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if readErr != io.EOF {
				return nil, fmt.Errorf("read observation log %q: %w", path, readErr)
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
		rec, parseErr := parseRecord(ctx, line)
		if parseErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("invalid observation log %q at complete line %d: %w", path, lineNumber, parseErr)
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

func readCompleteLine(ctx context.Context, reader *bufio.Reader) ([]byte, bool, error) {
	var line []byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		part, err := reader.ReadSlice('\n')
		line = append(line, part...)
		switch err {
		case nil:
			return line, true, nil
		case bufio.ErrBufferFull:
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
