package runtime

import (
	"context"
	"io"
	"os"
)

type openFileResult struct {
	file *os.File
	err  error
}

// openFileContext lets its caller stop waiting when a filesystem open blocks,
// as it can for a named pipe. The open syscall itself cannot be interrupted;
// if it finishes after cancellation, this goroutine closes the file without
// handing it to the caller.
func openFileContext(ctx context.Context, path string, flag int, perm os.FileMode) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make(chan openFileResult)
	accepted := make(chan struct{})
	go func() {
		file, err := os.OpenFile(path, flag, perm)
		opened := openFileResult{file: file, err: err}
		if ctx.Err() != nil {
			if file != nil {
				_ = file.Close()
			}
			return
		}
		select {
		case result <- opened:
			select {
			case <-accepted:
			case <-ctx.Done():
				if file != nil {
					_ = file.Close()
				}
			}
		case <-ctx.Done():
			if file != nil {
				_ = file.Close()
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case opened := <-result:
		close(accepted)
		if err := ctx.Err(); err != nil {
			if opened.file != nil {
				_ = opened.file.Close()
			}
			return nil, err
		}
		return opened.file, opened.err
	}
}

func readTextFileContext(ctx context.Context, path string) (string, error) {
	data, err := readFileContext(ctx, path)
	return string(data), err
}

func readFileContext(ctx context.Context, path string) ([]byte, error) {
	file, err := openFileContext(ctx, path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = file.Close() })
	defer func() {
		stopClose()
		_ = file.Close()
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	return data, err
}

func writeTextFileContext(ctx context.Context, path, text string) error {
	return writeFileContext(ctx, path, []byte(text))
}

func writeFileContext(ctx context.Context, path string, data []byte) error {
	file, err := openFileContext(ctx, path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = file.Close() })
	defer func() {
		stopClose()
		_ = file.Close()
	}()
	remaining := data
	for len(remaining) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		written, err := file.Write(remaining)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		remaining = remaining[written:]
	}
	return ctx.Err()
}
