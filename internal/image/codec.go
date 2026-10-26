// Package imageapi contains the file codec shared by image input and output.
package imageapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// ReadData validates provider-returned image bytes and keeps an independent
// snapshot so callers may safely reuse or modify the input slice.
func ReadData(ctx context.Context, path string, content []byte, detail string) (*Data, error) {
	return readData(ctx, path, content, detail, "response")
}

// ReadInputData validates bytes supplied by the execution filesystem. Invalid
// file content is an input error, independently of the chosen FS backend.
func ReadInputData(ctx context.Context, path string, content []byte, detail string) (*Data, error) {
	return readData(ctx, path, content, detail, "input")
}

func readData(ctx context.Context, path string, content []byte, detail, invalidImageCode string) (*Data, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, &Error{Code: "input", Message: "image path must be nonempty"}
	}
	if detail == "" {
		detail = "auto"
	}
	if detail != "auto" && detail != "low" && detail != "high" {
		return nil, &Error{Code: "input", Message: "image detail must be auto, low, or high"}
	}
	snapshot := append([]byte(nil), content...)
	mimeType, width, height, err := inspectImage(snapshot)
	if err != nil {
		return nil, &Error{Code: invalidImageCode, Message: "invalid or unsupported image: " + err.Error()}
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return &Data{
		Metadata: Metadata{
			Path: path, MIMEType: mimeType, Bytes: len(snapshot),
			Width: width, Height: height, Detail: detail,
		},
		Content: snapshot,
	}, nil
}

func inspectImage(content []byte) (mimeType string, width, height int, err error) {
	decoded, format, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return "", 0, 0, err
	}
	var ok bool
	switch format {
	case "png":
		mimeType, ok = "image/png", true
	case "jpeg":
		mimeType, ok = "image/jpeg", true
	case "webp":
		mimeType, ok = "image/webp", true
	}
	if !ok {
		return "", 0, 0, fmt.Errorf("unsupported image format %q", format)
	}
	width, height = decoded.Bounds().Dx(), decoded.Bounds().Dy()
	if width <= 0 || height <= 0 {
		return "", 0, 0, errors.New("image dimensions must be positive")
	}
	return mimeType, width, height, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return &Error{Code: "input", Message: "image operation requires a context"}
	}
	if err := ctx.Err(); err != nil {
		return &Error{Code: "cancelled", Message: err.Error()}
	}
	return nil
}
