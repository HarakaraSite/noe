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
	"testing"
)

func TestReadDataSnapshotsSupportedImages(t *testing.T) {
	tests := []struct {
		name, mime    string
		width, height int
		content       []byte
	}{
		{name: "png", mime: "image/png", width: 1, height: 1, content: encodePNG(t)},
		{name: "jpeg", mime: "image/jpeg", width: 1, height: 1, content: encodeJPEG(t)},
		{name: "webp", mime: "image/webp", width: 75, height: 100, content: decodeFixture(t, "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==")},
	}
	for _, reader := range []struct {
		name string
		read func(context.Context, string, []byte, string) (*Data, error)
	}{{"input", ReadInputData}, {"provider", ReadData}} {
		t.Run(reader.name, func(t *testing.T) {
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					path := "image." + test.name
					content := append([]byte(nil), test.content...)
					data, err := reader.read(context.Background(), path, content, "high")
					if err != nil {
						t.Fatal(err)
					}
					if data.Path != path || data.MIMEType != test.mime || data.Bytes != len(test.content) || data.Detail != "high" {
						t.Fatalf("unexpected metadata: %+v", data.Metadata)
					}
					if data.Width != test.width || data.Height != test.height {
						t.Fatalf("unexpected dimensions: got %dx%d, want %dx%d", data.Width, data.Height, test.width, test.height)
					}
					if !bytes.Equal(data.Content, test.content) {
						t.Fatal("decoded content differs from the supplied snapshot")
					}
					content[0] ^= 0xff
					if !bytes.Equal(data.Content, test.content) {
						t.Fatal("snapshot changed after the source buffer was modified")
					}
				})
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
	for _, reader := range []struct {
		name, invalidCode string
		read              func(context.Context, string, []byte, string) (*Data, error)
	}{{"input", "input", ReadInputData}, {"provider", "response", ReadData}} {
		t.Run(reader.name, func(t *testing.T) {
			for _, test := range []struct {
				name, path, detail, code string
				content                  []byte
			}{
				{name: "truncated image", path: "input", detail: "auto", content: valid[:len(valid)-8], code: reader.invalidCode},
				{name: "unknown format", path: "input", detail: "auto", content: []byte("not an image"), code: reader.invalidCode},
				{name: "invalid detail", path: "input", detail: "medium", content: valid, code: "input"},
				{name: "empty path", content: valid, code: "input"},
			} {
				t.Run(test.name, func(t *testing.T) {
					_, err := reader.read(context.Background(), test.path, test.content, test.detail)
					var imageErr *Error
					if !errors.As(err, &imageErr) || imageErr.Code != test.code || imageErr.Message == "" {
						t.Fatalf("expected %s image error with message, got %v", test.code, err)
					}
				})
			}
		})
	}
}

func TestReadRequiresLiveContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	content := encodePNG(t)
	for _, read := range []func(context.Context, string, []byte, string) (*Data, error){ReadInputData, ReadData} {
		for _, test := range []struct {
			ctx  context.Context
			code string
		}{{nil, "input"}, {ctx, "cancelled"}} {
			_, err := read(test.ctx, "image.png", content, "auto")
			var imageErr *Error
			if !errors.As(err, &imageErr) || imageErr.Code != test.code {
				t.Fatalf("expected %s image error, got %v", test.code, err)
			}
		}
	}
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
