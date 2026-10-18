package imageapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileSnapshotsSupportedImages(t *testing.T) {
	tests := []struct {
		name, mime    string
		width, height int
		content       []byte
	}{
		{name: "png", mime: "image/png", width: 1, height: 1, content: encodePNG(t)},
		{name: "jpeg", mime: "image/jpeg", width: 1, height: 1, content: encodeJPEG(t)},
		{name: "webp", mime: "image/webp", content: decodeFixture(t, "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "image."+test.name)
			if err := os.WriteFile(path, test.content, 0o600); err != nil {
				t.Fatal(err)
			}
			data, err := ReadFile(context.Background(), path, "high")
			if err != nil {
				t.Fatal(err)
			}
			if data.Path != path || data.MIMEType != test.mime || data.Bytes != len(test.content) || data.Detail != "high" {
				t.Fatalf("unexpected metadata: %+v", data.Metadata)
			}
			if test.width > 0 && data.Width != test.width || test.height > 0 && data.Height != test.height {
				t.Fatalf("unexpected dimensions: got %dx%d, want %dx%d", data.Width, data.Height, test.width, test.height)
			}
			if data.Width <= 0 || data.Height <= 0 {
				t.Fatalf("invalid dimensions: %dx%d", data.Width, data.Height)
			}
			if !bytes.Equal(data.Content, test.content) {
				t.Fatal("read content differs from the file snapshot")
			}
			if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data.Content, test.content) {
				t.Fatal("snapshot changed after the source file was modified")
			}
		})
	}
}

func TestReadDataValidatesAndCopiesProviderOutput(t *testing.T) {
	content := encodePNG(t)
	data, err := ReadData(context.Background(), "/tmp/provider-output.png", content, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.Detail != "auto" || data.MIMEType != "image/png" || data.Width != 1 || data.Height != 1 {
		t.Fatalf("unexpected metadata: %+v", data.Metadata)
	}
	content[0] ^= 0xff
	if bytes.Equal(data.Content, content) {
		t.Fatal("returned content aliases the provider buffer")
	}
}

func TestReadRejectsInvalidImagesAndDetail(t *testing.T) {
	valid := encodePNG(t)
	for _, test := range []struct {
		name, detail string
		content      []byte
	}{
		{name: "truncated image", detail: "auto", content: valid[:len(valid)-8]},
		{name: "unknown format", detail: "auto", content: []byte("not an image")},
		{name: "invalid detail", detail: "medium", content: valid},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ReadData(context.Background(), "/tmp/input", test.content, test.detail)
			if err == nil {
				t.Fatal("expected an error")
			}
			var imageErr *Error
			if !errors.As(err, &imageErr) {
				t.Fatalf("expected image error, got %T: %v", err, err)
			}
			if test.name == "invalid detail" && imageErr.Code != "input" {
				t.Fatalf("invalid detail code = %q, want input", imageErr.Code)
			}
		})
	}
}

func TestReadFileRequiresAbsolutePathAndContext(t *testing.T) {
	if _, err := ReadFile(context.Background(), "relative.png", "auto"); err == nil {
		t.Fatal("expected relative path to fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadFile(ctx, filepath.Join(t.TempDir(), "image.png"), "auto"); err == nil {
		t.Fatal("expected canceled read to fail")
	}
}

func TestSaveFileAtomicallyReplacesOnlyAfterValidation(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "result.png")
	original := encodePNG(t)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := SaveFile(context.Background(), path, []byte("bad image")); err == nil {
		t.Fatal("expected invalid image to fail")
	}
	assertFileContent(t, path, original)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SaveFile(ctx, path, encodeJPEG(t)); err == nil {
		t.Fatal("expected canceled save to fail")
	}
	assertFileContent(t, path, original)

	jpegBytes := encodeJPEG(t)
	metadata, err := SaveFile(context.Background(), path, jpegBytes)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Path != path || metadata.MIMEType != "image/jpeg" || metadata.Bytes != len(jpegBytes) || metadata.Width != 1 || metadata.Height != 1 {
		t.Fatalf("unexpected saved metadata: %+v", metadata)
	}
	assertFileContent(t, path, jpegBytes)
}

func TestSaveFileFailureLeavesDestinationUntouched(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "existing-directory")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(destination, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveFile(context.Background(), destination, encodePNG(t)); err == nil {
		t.Fatal("expected replacement of a directory to fail")
	}
	assertFileContent(t, marker, []byte("keep"))
}

func encodePNG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, testImage()); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func encodeJPEG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, testImage(), nil); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 220, G: 40, B: 30, A: 255})
	return img
}

func decodeFixture(t *testing.T, encoded string) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertFileContent(t *testing.T, path string, expected []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, expected) {
		t.Fatalf("file content changed: got %d bytes, want %d", len(got), len(expected))
	}
}
