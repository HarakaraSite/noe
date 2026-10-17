package credentials

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

func WithFileLock(ctx context.Context, path string, action func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err = tryLock(f)
		if err == nil {
			break
		}
		if !errors.Is(err, errLockBusy) {
			return err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer unlock(f)
	return action()
}
