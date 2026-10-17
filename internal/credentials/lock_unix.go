//go:build !windows

package credentials

import (
	"errors"
	"os"
	"syscall"
)

var errLockBusy = syscall.EWOULDBLOCK

func tryLock(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EAGAIN) {
		return errLockBusy
	}
	return err
}

func unlock(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
