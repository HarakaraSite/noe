package noeprofile

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
)

type profileImageEditor func(context.Context, model.ImageEditRequest) (model.GeneratedImage, error)

func (f profileImageEditor) EditImage(ctx context.Context, q model.ImageEditRequest) (model.GeneratedImage, error) {
	return f(ctx, q)
}

func TestLazyModelsPreservesOptionalImageEditor(t *testing.T) {
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewNRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	content := out.Bytes()
	called := false
	service := Models(model.Options{ImageEditor: profileImageEditor(func(ctx context.Context, q model.ImageEditRequest) (model.GeneratedImage, error) {
		called = true
		if len(q.Images) != 1 || q.Images[0].MIMEType != "image/png" || !bytes.Equal(q.Images[0].Content, content) {
			t.Fatal("CLI lazy service changed the edit input")
		}
		return model.GeneratedImage{Content: content, RequestID: "edited"}, nil
	})})
	editor, ok := service.(model.ImageEditor)
	if !ok {
		t.Fatal("CLI lazy model service lost optional editing capability")
	}
	result, err := editor.EditImage(t.Context(), model.ImageEditRequest{Provider: "fixture", Model: "image", Prompt: "edit", Images: []model.ImageInput{{Content: content}}})
	if err != nil || !called || result.RequestID != "edited" {
		t.Fatalf("lazy edit: called=%t result=%+v err=%v", called, result, err)
	}
}
