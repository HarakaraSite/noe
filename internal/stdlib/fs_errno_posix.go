//go:build !windows

package stdlib

import "syscall"

func filesystemPlatformErrorCode(syscall.Errno) string { return "" }
