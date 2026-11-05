package imageapi

import (
	"context"
	jsonv1 "encoding/json"
	"fmt"
	"strings"
)

// Input owns a snapshot of an edit source. File access belongs to the binding.
type Input struct {
	MIMEType string `json:"-"`
	Content  []byte `json:"-"`
}

// EditRequest asks for one edited image without choosing an output location.
type EditRequest struct {
	Provider   string                       `json:"provider"`
	Model      string                       `json:"model"`
	Prompt     string                       `json:"prompt"`
	Images     []Input                      `json:"-"`
	Parameters map[string]jsonv1.RawMessage `json:"parameters,omitempty"`
	RunID      string                       `json:"-"`
}

type Editor interface {
	EditImage(context.Context, EditRequest) (Generated, error)
}

// PrepareEdit validates bytes and fixes the snapshots used by a request.
func PrepareEdit(ctx context.Context, request EditRequest) (EditRequest, error) {
	if err := (Request{Provider: request.Provider, Model: request.Model, Prompt: request.Prompt, Parameters: request.Parameters}).ValidateGeneration(); err != nil {
		return EditRequest{}, DescribeError(err, "edit", "input", Generated{})
	}
	if len(request.Images) == 0 {
		return EditRequest{}, &Error{Code: "input", Message: "images must contain at least one image", Operation: "edit", Stage: "input"}
	}
	for name := range request.Parameters {
		if strings.EqualFold(name, "mask") {
			return EditRequest{}, &Error{Code: "unsupported", Message: "mask editing is unsupported", Operation: "edit", Stage: "input"}
		}
	}
	images := make([]Input, len(request.Images))
	for i, input := range request.Images {
		data, err := ReadInputData(ctx, fmt.Sprintf("image[%d]", i), input.Content, "")
		if err != nil {
			return EditRequest{}, DescribeError(err, "edit", "input", Generated{})
		}
		if input.MIMEType != "" && input.MIMEType != data.MIMEType {
			return EditRequest{}, &Error{Code: "input", Message: "input MIME type does not match image bytes", Operation: "edit", Stage: "input"}
		}
		images[i] = Input{MIMEType: data.MIMEType, Content: data.Content}
	}
	request.Images = images
	return request, nil
}

// DescribeError preserves provider metadata while adding the local operation/stage.
func DescribeError(err error, operation, stage string, generated Generated) *Error {
	details := *AsError(err)
	if details.Operation == "" {
		details.Operation = operation
	}
	if details.Stage == "" {
		details.Stage = stage
	}
	if len(details.Usage) == 0 {
		details.Usage = append(jsonv1.RawMessage(nil), generated.Usage...)
	}
	if details.RequestID == "" {
		details.RequestID = generated.RequestID
	}
	return &details
}
