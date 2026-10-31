package modelbinding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
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
	if !ok || imageErr.Code != "input" || imageErr.Operation != "read" || imageErr.Stage != "input" || imageErr.Message != "image input must be a regular file" {
		t.Fatalf("unexpected non-regular input error: %+v", value)
	}
}

type editBindingService struct {
	calls   int
	inputs  [][]byte
	content []byte
	edit    func(context.Context, imageapi.EditRequest) (imageapi.Generated, error)
}

func (*editBindingService) ListModels(context.Context, provider.ModelQuery) ([]provider.Model, error) {
	return nil, nil
}
func (*editBindingService) Generate(context.Context, provider.Request) (provider.Response, error) {
	return provider.Response{}, nil
}
func (*editBindingService) Decide(context.Context, decision.Request) (decision.Response, error) {
	return decision.Response{}, nil
}
func (*editBindingService) GenerateImage(context.Context, imageapi.GenerationRequest) (imageapi.Generated, error) {
	return imageapi.Generated{}, nil
}
func (s *editBindingService) EditImage(ctx context.Context, request imageapi.EditRequest) (imageapi.Generated, error) {
	s.calls++
	if s.edit != nil {
		return s.edit(ctx, request)
	}
	for _, input := range request.Images {
		if input.MIMEType != "image/png" {
			return imageapi.Generated{}, errors.New("input MIME type was not inferred")
		}
		s.inputs = append(s.inputs, append([]byte(nil), input.Content...))
	}
	if len(request.Images) > 0 {
		request.Images[0].Content[0] ^= 0xff
	}
	return imageapi.Generated{
		Content: append([]byte(nil), s.content...), RequestID: "edit-request",
		Usage: json.RawMessage(`{"total_tokens":9}`),
	}, nil
}

func bindingPNG(t *testing.T, pixel color.Color) []byte {
	t.Helper()
	var output bytes.Buffer
	imageData := image.NewRGBA(image.Rect(0, 0, 2, 2))
	imageData.Set(0, 0, pixel)
	if err := png.Encode(&output, imageData); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func newEditBinding(t *testing.T, root string, readonly bool, service *editBindingService) *Binding {
	t.Helper()
	rt, err := stdlib.NewRuntime(t.Context(), stdlib.Profile{
		Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: readonly}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	return &Binding{view: rt.Bindings(), service: service}
}

func callEditImage(t *testing.T, binding *Binding, paths []string, output string) any {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"provider": "fixture", "model": "image", "prompt": "edit",
		"output": output, "images": paths,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := binding.call(t.Context(), "edit_image", string(payload))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestEditImageUsesExecutionFSOrderAndPrivateSnapshots(t *testing.T) {
	root := t.TempDir()
	first := bindingPNG(t, color.RGBA{R: 255, A: 255})
	second := bindingPNG(t, color.RGBA{B: 255, A: 255})
	for name, data := range map[string][]byte{"first.png": first, "second.png": second} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := &editBindingService{content: first}
	binding := newEditBinding(t, root, false, service)
	value := callEditImage(t, binding, []string{"/second.png", "/first.png"}, "/edited.png")
	response, ok := value.(imageapi.Response)
	if !ok || response.Path != "/edited.png" || response.MIMEType != "image/png" || response.RequestID != "edit-request" || len(response.Usage) == 0 {
		t.Fatalf("edit response=%+v", value)
	}
	if service.calls != 1 || len(service.inputs) != 2 || !bytes.Equal(service.inputs[0], second) || !bytes.Equal(service.inputs[1], first) {
		t.Fatalf("provider input order/snapshots calls=%d inputs=%v", service.calls, service.inputs)
	}
	for name, expected := range map[string][]byte{"first.png": first, "second.png": second, "edited.png": first} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("%s changed: equal=%v err=%v", name, bytes.Equal(got, expected), err)
		}
	}
}

func TestEditImageValidatesRootAndWritableFilesystemBeforeProvider(t *testing.T) {
	root := t.TempDir()
	content := bindingPNG(t, color.RGBA{G: 255, A: 255})
	if err := os.WriteFile(filepath.Join(root, "input.png"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		readonly bool
		paths    []string
		code     string
		stage    string
	}{
		{name: "root escape", paths: []string{"../../outside.png"}, code: "filesystem", stage: "input"},
		{name: "read-only output", readonly: true, paths: []string{"/input.png"}, code: "filesystem", stage: "save"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &editBindingService{content: content}
			binding := newEditBinding(t, root, test.readonly, service)
			value := callEditImage(t, binding, test.paths, "/out.png")
			result, ok := value.(map[string]any)
			imageErr, typed := result["imageError"].(*imageapi.Error)
			if !ok || !typed || imageErr.Code != test.code || imageErr.Operation != "edit" || imageErr.Stage != test.stage {
				t.Fatalf("unexpected error: %+v", value)
			}
			if service.calls != 0 {
				t.Fatalf("rejected edit called provider %d times", service.calls)
			}
		})
	}
}

func TestEditImageRejectsUnknownAndUnsupportedOptions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "input.png"), bindingPNG(t, color.RGBA{A: 255}), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &editBindingService{content: bindingPNG(t, color.RGBA{A: 255})}
	binding := newEditBinding(t, root, false, service)
	for name, payload := range map[string]string{
		"unknown file option": `{"provider":"fixture","model":"image","prompt":"edit","output":"out.png","images":["input.png"],"mask":"mask.png"}`,
		"mask parameter":      `{"provider":"fixture","model":"image","prompt":"edit","output":"out.png","images":["input.png"],"parameters":{"mask":{}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			value, err := binding.call(t.Context(), "edit_image", payload)
			if err != nil {
				t.Fatal(err)
			}
			result, ok := value.(map[string]any)
			imageErr, typed := result["imageError"].(*imageapi.Error)
			if !ok || !typed || imageErr.Operation != "edit" || imageErr.Stage != "input" {
				t.Fatalf("unexpected request error: %+v", value)
			}
			if name == "mask parameter" && imageErr.Code != "unsupported" {
				t.Fatalf("mask code=%q; want unsupported", imageErr.Code)
			}
			if service.calls != 0 {
				t.Fatalf("invalid edit called provider %d times", service.calls)
			}
		})
	}
}

func TestEditImageCancellationPreventsSaveAndKeepsKnownMetadata(t *testing.T) {
	root := t.TempDir()
	input := bindingPNG(t, color.RGBA{R: 255, A: 255})
	previous := []byte("previous output")
	if err := os.WriteFile(filepath.Join(root, "input.png"), input, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "out.png"), previous, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	service := &editBindingService{content: input}
	service.edit = func(_ context.Context, _ imageapi.EditRequest) (imageapi.Generated, error) {
		cancel()
		return imageapi.Generated{Content: input, RequestID: "cancelled-edit", Usage: json.RawMessage(`{"total_tokens":11}`)}, nil
	}
	binding := newEditBinding(t, root, false, service)
	payload, err := json.Marshal(map[string]any{
		"provider": "fixture", "model": "image", "prompt": "edit",
		"output": "/out.png", "images": []string{"/input.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := binding.call(ctx, "edit_image", string(payload))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(map[string]any)
	imageErr, typed := result["imageError"].(*imageapi.Error)
	if !ok || !typed || imageErr.Code != "cancelled" || imageErr.Operation != "edit" || imageErr.RequestID != "cancelled-edit" || string(imageErr.Usage) != `{"total_tokens":11}` {
		t.Fatalf("canceled edit metadata=%+v", value)
	}
	if got, err := os.ReadFile(filepath.Join(root, "out.png")); err != nil || !bytes.Equal(got, previous) {
		t.Fatalf("canceled edit changed output: %q err=%v", got, err)
	}
}

func TestEditImagePromiseRetainsStructuredErrorWhenRunWrapperCancels(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "input.png"), bindingPNG(t, color.RGBA{G: 255, A: 255}), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &editBindingService{content: bindingPNG(t, color.RGBA{A: 255})}
	service.edit = func(context.Context, imageapi.EditRequest) (imageapi.Generated, error) {
		return imageapi.Generated{}, &imageapi.Error{
			Code: "transport", Message: "provider stopped", Operation: "edit", Stage: "request",
			RequestID: "provider-error-id", Usage: json.RawMessage(`{"total_tokens":13}`),
		}
	}
	bindCtx, cancelBinding := context.WithCancel(t.Context())
	t.Cleanup(cancelBinding)
	_, err := stdlib.Execute(t.Context(), stdlib.Source{
		Name: "[modelbinding edit cancellation]", Virtual: true, Module: true,
		Code: `import {edit_image} from "noe:image";
try { await edit_image({provider:"fixture",model:"image",prompt:"edit",images:["/input.png"],output:"/out.png"}); throw Error("edit succeeded") }
catch(error) {
  if(error.name!=="ImageError"||error.code!=="cancelled"||error.operation!=="edit"||error.stage!=="request"||error.requestId!=="provider-error-id"||error.usage?.total_tokens!==13)throw error;
}`,
	}, stdlib.Profile{
		Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root}},
		Initialize: func(ctx context.Context, view *stdlib.Bindings) (func(), error) {
			binding, bindErr := Bind(bindCtx, view, service, Options{Run: func(ctx context.Context, work func(context.Context) (any, error)) (any, error) {
				value, workErr := work(ctx)
				if workErr != nil {
					return value, workErr
				}
				cancelBinding()
				return nil, context.Canceled
			}})
			if bindErr != nil {
				return nil, bindErr
			}
			return binding.Close, nil
		},
	})
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	if service.calls != 1 {
		t.Fatalf("edit provider calls=%d", service.calls)
	}
	if _, err := os.Stat(filepath.Join(root, "out.png")); !os.IsNotExist(err) {
		t.Fatalf("provider error unexpectedly wrote output: %v", err)
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
