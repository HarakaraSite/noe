package stdlib

import (
	"errors"
	"os"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func (f *fsHost) installMetadata() error {
	var err error
	f.date, err = f.rt.EvalFile("<fs-date>", `(() => { const DateValue = Date; return milliseconds => new DateValue(milliseconds); })()`)
	return err
}

func fileModeBits(mode os.FileMode) int {
	bits := fileKind(mode) | int(mode.Perm())
	if mode&os.ModeSetuid != 0 {
		bits |= 0o4000
	}
	if mode&os.ModeSetgid != 0 {
		bits |= 0o2000
	}
	if mode&os.ModeSticky != 0 {
		bits |= 0o1000
	}
	return bits
}

// statValue shares the representation for path metadata and open descriptors.
func (f *fsHost) statValue(info os.FileInfo) (quickjs.Value, error) {
	o := f.rt.NewObject()
	mode := info.Mode()
	stamp := info.ModTime()
	// UnixNano overflows for dates outside approximately 1678–2262.
	milliseconds := float64(stamp.Unix())*1e3 + float64(stamp.Nanosecond())/1e6
	date, err := f.date.Call(milliseconds)
	if err != nil {
		return quickjs.Value{}, err
	}
	if err := errors.Join(
		o.Set("size", float64(info.Size())),
		o.Set("mode", fileModeBits(mode)),
		o.Set("mtimeMs", milliseconds),
		o.Set("mtime", date),
		o.Set("isFile", func() bool { return mode.IsRegular() }),
		o.Set("isDirectory", func() bool { return mode.IsDir() }),
		o.Set("isSymbolicLink", func() bool { return mode&os.ModeSymlink != 0 }),
	); err != nil {
		return quickjs.Value{}, err
	}
	return o, nil
}
