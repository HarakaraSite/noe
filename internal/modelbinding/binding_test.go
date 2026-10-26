package modelbinding

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func imageReadBinding(t *testing.T, root string) *Binding {
	t.Helper()
	rt, err := stdlib.NewRuntime(t.Context(), stdlib.Profile{
		Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	return &Binding{view: rt.Bindings(), options: Options{
		ReadImage: func(ctx context.Context, path, detail string, _ int, read func(context.Context, string) ([]byte, error)) (imageapi.Metadata, error) {
			content, err := read(ctx, path)
			if err != nil {
				return imageapi.Metadata{}, err
			}
			data, err := imageapi.ReadInputData(ctx, path, content, detail)
			if err != nil {
				return imageapi.Metadata{}, err
			}
			return data.Metadata, nil
		},
	}}
}

func callImageRead(t *testing.T, binding *Binding, path string) any {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"path": path, "detail": "auto", "ordinal": 1})
	if err != nil {
		t.Fatal(err)
	}
	value, err := binding.call(t.Context(), "read_image", string(payload))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertNonRegularImageRead(t *testing.T, root, path string) {
	t.Helper()
	value := callImageRead(t, imageReadBinding(t, root), path)
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("non-regular input accepted: %+v", value)
	}
	imageErr, ok := result["imageError"].(*imageapi.Error)
	if !ok || imageErr.Code != "input" || imageErr.Message != "image input must be a regular file" {
		t.Fatalf("unexpected non-regular input error: %+v", value)
	}
}

func TestImageReadUsesSelectedFilesystemForRegularFiles(t *testing.T) {
	root := t.TempDir()
	var content bytes.Buffer
	if err := png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "image.png"), content.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	binding := imageReadBinding(t, root)
	check := func(t *testing.T, path string) {
		t.Helper()
		value := callImageRead(t, binding, path)
		metadata, ok := value.(imageapi.Metadata)
		if !ok || metadata.Path != path || metadata.MIMEType != "image/png" ||
			metadata.Width != 2 || metadata.Height != 3 || metadata.Bytes != content.Len() {
			t.Fatalf("selected filesystem image read failed: %+v", value)
		}
	}
	t.Run("regular file in read-only root", func(t *testing.T) { check(t, "/image.png") })
	t.Run("symlink to regular file", func(t *testing.T) {
		if err := os.Symlink("image.png", filepath.Join(root, "alias.png")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		check(t, "/alias.png")
	})
}

func TestImageReadRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "directory.png"), 0o700); err != nil {
		t.Fatal(err)
	}
	assertNonRegularImageRead(t, root, "/directory.png")
}

func TestImageReadReaderPreservesCancellation(t *testing.T) {
	binding := imageReadBinding(t, t.TempDir())
	binding.options.ReadImage = func(ctx context.Context, path, _ string, _ int, read func(context.Context, string) ([]byte, error)) (imageapi.Metadata, error) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := read(ctx, path)
		return imageapi.Metadata{}, err
	}
	value := callImageRead(t, binding, "/image.png")
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("canceled image read succeeded: %+v", value)
	}
	imageErr, ok := result["imageError"].(*imageapi.Error)
	if !ok || imageErr.Code != "cancelled" {
		t.Fatalf("unexpected canceled image read error: %+v", value)
	}
}
