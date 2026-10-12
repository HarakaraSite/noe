package runtime

import (
	"context"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func (h *host) installBinaryFiles(noe quickjs.Value) error {
	if err := noe.Set("readFile", func(path string) quickjs.Value {
		path = h.filePath(path)
		return h.startAsyncWork(func(ctx context.Context) (any, error) {
			data, err := readFileContext(ctx, path)
			if err != nil {
				return nil, err
			}
			return binaryResult(data), nil
		})
	}); err != nil {
		return err
	}

	write := func(path string, value quickjs.Value) (quickjs.Value, error) {
		view, ok := value.Bytes()
		if !ok {
			return quickjs.Value{}, h.rt.ThrowTypeError("noe.writeFile requires a non-detached Uint8Array")
		}
		data := append([]byte(nil), view...)
		path = h.filePath(path)
		return h.startAsyncWork(func(ctx context.Context) (any, error) {
			return nil, writeFileContext(ctx, path, data)
		}), nil
	}

	factory, err := h.rt.EvalFile("<noe-binary-files>", `(write => {
		const typedArrayTag = Object.getOwnPropertyDescriptor(
			Object.getPrototypeOf(Uint8Array.prototype), Symbol.toStringTag
		).get;
		const apply = Reflect.apply;
		return async function writeFile(path, bytes) {
			if (apply(typedArrayTag, bytes, []) !== "Uint8Array") {
				throw new TypeError("noe.writeFile requires a Uint8Array");
			}
			await write(path, bytes);
		};
	})`)
	if err != nil {
		return err
	}
	writer, err := factory.Call(write)
	if err != nil {
		return err
	}
	return noe.Set("writeFile", writer)
}
