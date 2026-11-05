//go:build windows

package stdlib

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// Windows filesystem calls report native Win32 errors, while syscall's POSIX
// constants are mostly synthetic. Normalize the common native values first.
func filesystemPlatformErrorCode(errno syscall.Errno) string {
	switch errno {
	case windows.ERROR_FILE_NOT_FOUND, windows.ERROR_PATH_NOT_FOUND,
		windows.ERROR_INVALID_NAME, windows.ERROR_BAD_PATHNAME, windows.ERROR_INVALID_DRIVE:
		return "ENOENT"
	case windows.ERROR_FILE_EXISTS, windows.ERROR_ALREADY_EXISTS:
		return "EEXIST"
	case windows.ERROR_NOT_SAME_DEVICE:
		return "EXDEV"
	case windows.ERROR_ACCESS_DENIED:
		return "EACCES"
	case windows.ERROR_PRIVILEGE_NOT_HELD:
		return "EPERM"
	case windows.ERROR_OPERATION_ABORTED:
		return "ECANCELED"
	case windows.ERROR_INVALID_HANDLE:
		return "EBADF"
	case windows.ERROR_TOO_MANY_OPEN_FILES:
		return "EMFILE"
	case windows.ERROR_NOT_ENOUGH_MEMORY, windows.ERROR_OUTOFMEMORY:
		return "ENOMEM"
	case windows.ERROR_WRITE_PROTECT:
		return "EROFS"
	case windows.ERROR_SHARING_VIOLATION, windows.ERROR_LOCK_VIOLATION:
		return "EBUSY"
	case windows.ERROR_HANDLE_DISK_FULL, windows.ERROR_DISK_FULL:
		return "ENOSPC"
	case windows.ERROR_NOT_SUPPORTED:
		return "ENOTSUP"
	case windows.ERROR_INVALID_PARAMETER:
		return "EINVAL"
	case windows.ERROR_BROKEN_PIPE:
		return "EPIPE"
	case windows.ERROR_DIR_NOT_EMPTY:
		return "ENOTEMPTY"
	case windows.ERROR_FILENAME_EXCED_RANGE:
		return "ENAMETOOLONG"
	case windows.ERROR_DIRECTORY:
		return "ENOTDIR"
	default:
		return ""
	}
}
