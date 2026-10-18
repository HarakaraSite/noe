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
	"io"
	"os"
	"path/filepath"

	_ "golang.org/x/image/webp"
)

// ReadFile reads and validates an image at an already-resolved absolute path.
// The returned content is a snapshot and does not depend on later file changes.
func ReadFile(ctx context.Context, path, detail string) (*Data, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if path == "" || !filepath.IsAbs(path) {
		return nil, &Error{Code: "input", Message: "image path must be an absolute path"}
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, &Error{Code: "filesystem", Message: "stat image: " + err.Error()}
	}
	if !info.Mode().IsRegular() {
		return nil, &Error{Code: "input", Message: "image input must be a regular file"}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if contextErr := contextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		return nil, &Error{Code: "filesystem", Message: "read image: " + err.Error()}
	}
	return readData(ctx, path, content, detail, "input")
}

// ReadData validates provider-returned image bytes and keeps an independent
// snapshot so callers may safely reuse or modify the input slice.
func ReadData(ctx context.Context, path string, content []byte, detail string) (*Data, error) {
	return readData(ctx, path, content, detail, "response")
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

// SaveFile validates the complete image before writing it. It writes a sibling
// temporary file and commits with rename, leaving an existing destination
// intact if validation, writing, syncing, closing, or cancellation fails.
func SaveFile(ctx context.Context, path string, content []byte) (Metadata, error) {
	if err := contextError(ctx); err != nil {
		return Metadata{}, err
	}
	if path == "" {
		return Metadata{}, &Error{Code: "input", Message: "image output path must be nonempty"}
	}
	snapshot := append([]byte(nil), content...)
	mimeType, width, height, err := inspectImage(snapshot)
	if err != nil {
		return Metadata{}, &Error{Code: "response", Message: "invalid generated image: " + err.Error()}
	}
	if err := contextError(ctx); err != nil {
		return Metadata{}, err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".noe-image-*.tmp")
	if err != nil {
		if contextErr := contextError(ctx); contextErr != nil {
			return Metadata{}, contextErr
		}
		return Metadata{}, &Error{Code: "filesystem", Message: "create image temporary file: " + err.Error()}
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := writeContext(ctx, temporary, snapshot); err != nil {
		_ = temporary.Close()
		return Metadata{}, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		if contextErr := contextError(ctx); contextErr != nil {
			return Metadata{}, contextErr
		}
		return Metadata{}, &Error{Code: "filesystem", Message: "sync image temporary file: " + err.Error()}
	}
	if err := contextError(ctx); err != nil {
		_ = temporary.Close()
		return Metadata{}, err
	}
	if err := temporary.Close(); err != nil {
		if contextErr := contextError(ctx); contextErr != nil {
			return Metadata{}, contextErr
		}
		return Metadata{}, &Error{Code: "filesystem", Message: "close image temporary file: " + err.Error()}
	}
	if err := contextError(ctx); err != nil {
		return Metadata{}, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if contextErr := contextError(ctx); contextErr != nil {
			return Metadata{}, contextErr
		}
		return Metadata{}, &Error{Code: "filesystem", Message: "replace image output: " + err.Error()}
	}
	committed = true
	return Metadata{
		Path: path, MIMEType: mimeType, Bytes: len(snapshot), Width: width, Height: height,
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

func writeContext(ctx context.Context, destination io.Writer, content []byte) error {
	const chunkSize = 64 * 1024
	for len(content) > 0 {
		if err := contextError(ctx); err != nil {
			return err
		}
		chunk := content
		if len(chunk) > chunkSize {
			chunk = chunk[:chunkSize]
		}
		written, err := destination.Write(chunk)
		if err != nil {
			if contextErr := contextError(ctx); contextErr != nil {
				return contextErr
			}
			return &Error{Code: "filesystem", Message: "write image temporary file: " + err.Error()}
		}
		if written != len(chunk) {
			return &Error{Code: "filesystem", Message: "write image temporary file: " + io.ErrShortWrite.Error()}
		}
		content = content[written:]
	}
	return contextError(ctx)
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
