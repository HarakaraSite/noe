package stdlib

import (
	"errors"
	"os"
	"syscall"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// filesystemErrorCode classifies OS errors independently of their localized
// message. Custom policy and argument codes remain distinct from OS failures.
func filesystemErrorCode(err error) string {
	var detail *fileIOError
	if errors.As(err, &detail) {
		if code, ok := detail.code.(string); ok {
			return code
		}
	}
	if errors.Is(err, filesystem.ErrReadOnly) {
		return "ERR_READ_ONLY"
	}
	if errors.Is(err, os.ErrClosed) {
		return "ERR_FILE_CLOSED"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if code := filesystemPlatformErrorCode(errno); code != "" {
			return code
		}
		if errno == syscall.EOPNOTSUPP {
			return "ENOTSUP"
		}
		if code, ok := filesystemErrnoNames[errno]; ok {
			return code
		}
	}
	// Windows filesystem calls use Win32 values rather than the POSIX constants.
	if errors.Is(err, os.ErrNotExist) {
		return "ENOENT"
	}
	if errors.Is(err, os.ErrExist) {
		return "EEXIST"
	}
	if errors.Is(err, os.ErrPermission) {
		return "EACCES"
	}
	if errors.Is(err, os.ErrInvalid) {
		return "EINVAL"
	}
	return "ERR_IO"
}

var filesystemErrnoNames = map[syscall.Errno]string{
	syscall.EPERM: "EPERM", syscall.ENOENT: "ENOENT", syscall.EIO: "EIO",
	syscall.EBADF: "EBADF", syscall.EAGAIN: "EAGAIN", syscall.ENOMEM: "ENOMEM",
	syscall.EACCES: "EACCES", syscall.EBUSY: "EBUSY", syscall.EEXIST: "EEXIST",
	syscall.EXDEV: "EXDEV", syscall.ENODEV: "ENODEV", syscall.ENOTDIR: "ENOTDIR",
	syscall.EISDIR: "EISDIR", syscall.EINVAL: "EINVAL", syscall.ENFILE: "ENFILE",
	syscall.EMFILE: "EMFILE", syscall.ETXTBSY: "ETXTBSY", syscall.EFBIG: "EFBIG",
	syscall.ENOSPC: "ENOSPC", syscall.ESPIPE: "ESPIPE", syscall.EROFS: "EROFS",
	syscall.EPIPE: "EPIPE", syscall.ENAMETOOLONG: "ENAMETOOLONG", syscall.ENOSYS: "ENOSYS",
	syscall.ENOTEMPTY: "ENOTEMPTY", syscall.ELOOP: "ELOOP", syscall.ENOTSUP: "ENOTSUP",
	syscall.EDQUOT: "EDQUOT", syscall.EILSEQ: "EILSEQ",
}

// errorValue runs on the runtime owner, as do sync throws and promise delivery.
func (f *fsHost) errorValue(err error) quickjs.Value {
	var thrown *quickjs.Error
	if errors.As(err, &thrown) {
		return thrown.Value()
	}

	// Let the engine retain the Go cause before adding JS fields. This preserves
	// errors.Is on uncaught exceptions returned to a Go embedding host.
	var value quickjs.Value
	if raise, encodeErr := f.rt.Encode(func() error { return err }); encodeErr == nil {
		_, caught := raise.Call()
		if errors.As(caught, &thrown) {
			value = thrown.Value()
		}
	}
	if !value.IsObject() {
		value = f.rt.NewError("Error", err.Error())
	}
	_ = value.Set("code", filesystemErrorCode(err))
	var errno syscall.Errno
	if errors.As(err, &errno) {
		_ = value.Set("errno", int64(errno))
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		_ = value.Set("path", f.show(pathError.Path))
		_ = value.Set("syscall", pathError.Op)
	}
	var linkError *os.LinkError
	if errors.As(err, &linkError) {
		_ = value.Set("path", f.show(linkError.Old))
		_ = value.Set("dest", f.show(linkError.New))
		_ = value.Set("syscall", linkError.Op)
	}
	return value
}

func (f *fsHost) throwError(err error) error {
	if err == nil {
		return nil
	}
	return f.rt.Throw(f.errorValue(err))
}

func filesystemResult[T any](f *fsHost, value T, err error) (T, error) {
	return value, f.throwError(err)
}

func (f *fsHost) reject(p *quickjs.Promise, err error) {
	_ = p.Reject(f.errorValue(err))
}
