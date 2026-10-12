package runtime

import (
	"context"
	"io"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func (h *host) installStdin(noe quickjs.Value, reader io.Reader) error {
	if reader == nil {
		return nil
	}
	// Calls happen only on the runtime owner. Share a single Promise rather
	// than start competing readers for the same stream.
	var started bool
	var promise quickjs.Value
	return noe.Set("readStdin", func() quickjs.Value {
		if !started {
			started = true
			promise = h.startAsyncWork(func(ctx context.Context) (any, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				data, err := io.ReadAll(reader)
				if contextErr := ctx.Err(); contextErr != nil {
					return nil, contextErr
				}
				if err != nil {
					return nil, err
				}
				return string(data), nil
			})
		}
		return promise
	})
}
