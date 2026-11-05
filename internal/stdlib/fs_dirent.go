package stdlib

import (
	_ "embed"
	"errors"
	"os"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed fs_dirent.js
var fsDirentJS string

type directoryEntries struct {
	parent  string
	entries []map[string]any
}

func (f *fsHost) installDirectoryValues() (quickjs.Value, error) {
	api, err := f.rt.EvalFile("<fs-dirent>", fsDirentJS)
	if err != nil {
		return quickjs.Value{}, err
	}
	f.dirents, err = api.Get("fromEntries")
	if err != nil {
		return quickjs.Value{}, err
	}
	return api.Get("Dirent")
}

func directoryOptions(opts quickjs.Value) (bool, error) {
	if opts.IsUndefined() || opts.IsNull() {
		return false, nil
	}
	if !opts.IsObject() {
		// Older installations ignored extra arguments. Only the new object
		// option selects a different result shape.
		return false, nil
	}
	value, err := opts.Get("withFileTypes")
	if err != nil {
		return false, err
	}
	if !value.IsUndefined() && value.Kind() != quickjs.KindBool {
		return false, fileError(errors.New("withFileTypes must be a boolean"), "ERR_INVALID_ARG")
	}
	return value.Bool(), nil
}

func (f *fsHost) readdir(p string, opts quickjs.Value) (quickjs.Value, error) {
	withTypes, err := directoryOptions(opts)
	if err != nil {
		return quickjs.Value{}, err
	}
	result, err := f.readDirectory(p, withTypes)
	if err != nil {
		return quickjs.Value{}, err
	}
	return f.directoryValue(result)
}

func (f *fsHost) readDirectory(p string, withTypes bool) (any, error) {
	full, err := f.resolve(p)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	if !withTypes {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return names, nil
	}
	values := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		values = append(values, map[string]any{"name": entry.Name(), "kind": fileKind(entry.Type())})
	}
	return directoryEntries{parent: p, entries: values}, nil
}

func (f *fsHost) directoryValue(result any) (quickjs.Value, error) {
	if entries, ok := result.(directoryEntries); ok {
		return f.dirents.Call(entries.entries, entries.parent)
	}
	return f.rt.Encode(result)
}

// File kinds use the portable POSIX type bits for both metadata and entries.
func fileKind(mode os.FileMode) int {
	switch {
	case mode.IsRegular():
		return 0o100000
	case mode.IsDir():
		return 0o040000
	case mode&os.ModeSymlink != 0:
		return 0o120000
	case mode&os.ModeNamedPipe != 0:
		return 0o010000
	case mode&os.ModeSocket != 0:
		return 0o140000
	case mode&os.ModeDevice != 0:
		if mode&os.ModeCharDevice != 0 {
			return 0o020000
		}
		return 0o060000
	default:
		return 0
	}
}
