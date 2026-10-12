package runtime

import (
	"context"
	"math"
	"os"
	"path/filepath"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func (h *host) installNoe(opts Options) error {
	noe := h.rt.NewObject()
	if err := h.installServe(noe, opts.Stderr); err != nil {
		return err
	}
	if err := h.installStdin(noe, opts.Stdin); err != nil {
		return err
	}
	if err := h.installFilesystem(noe); err != nil {
		return err
	}
	if err := h.installFileMutations(noe); err != nil {
		return err
	}
	if err := h.installBinaryFiles(noe); err != nil {
		return err
	}
	if err := noe.Set("args", append([]string{}, opts.Args...)); err != nil {
		return err
	}
	if err := noe.Set("cwd", func() string { return h.cwd }); err != nil {
		return err
	}
	undefined, err := h.rt.Get("undefined")
	if err != nil {
		return err
	}
	env := h.rt.NewObject()
	if err := env.Set("get", func(name string) (quickjs.Value, error) {
		value, ok := os.LookupEnv(name)
		if !ok {
			return undefined, nil
		}
		return h.rt.Encode(value)
	}); err != nil {
		return err
	}
	if err := noe.Set("env", env); err != nil {
		return err
	}
	if err := noe.Set("exit", func(code quickjs.Value) error {
		n := 0
		if !code.IsUndefined() {
			if code.Kind() != quickjs.KindNumber {
				return h.rt.ThrowTypeError("exit code must be an integer from 0 to 255")
			}
			value := code.Float()
			if math.IsNaN(value) || math.Trunc(value) != value || value < 0 || value > 255 {
				return h.rt.ThrowRangeError("exit code must be an integer from 0 to 255")
			}
			n = int(value)
		}
		h.exitRequested, h.exitCode = true, n
		return h.rt.Close()
	}); err != nil {
		return err
	}
	if err := noe.Set("readTextFile", func(path string) quickjs.Value {
		path = h.filePath(path)
		return h.startAsyncWork(func(ctx context.Context) (any, error) {
			return readTextFileContext(ctx, path)
		})
	}); err != nil {
		return err
	}
	write := func(path, text string) quickjs.Value {
		path = h.filePath(path)
		return h.startAsyncWork(func(ctx context.Context) (any, error) {
			return nil, writeTextFileContext(ctx, path, text)
		})
	}
	factory, err := h.rt.EvalFile("<noe-files>", `(write => async function writeTextFile(path, text) { await write(path, text); })`)
	if err != nil {
		return err
	}
	writer, err := factory.Call(write)
	if err != nil {
		return err
	}
	if err := noe.Set("writeTextFile", writer); err != nil {
		return err
	}
	return h.rt.Set("noe", noe)
}

func (h *host) filePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(h.cwd, path)
}
