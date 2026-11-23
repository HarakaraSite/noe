package provider

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"sort"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/jsonutil"
)

// responsesAccumulator keeps completed items and the terminal envelope, not
// the stream's delta history. Some providers put full items only in
// output_item.done, with an empty response.output in response.completed.
type responsesAccumulator struct {
	items       map[int]jsonv1.RawMessage
	envelope    map[string]jsonv1.RawMessage
	terminal    bool
	refusals    map[responsePartIndex]*responseRefusal
	failureKind ErrorKind
	err         error
}

type responsePartIndex struct {
	output  int
	content int
}

type responseRefusal struct {
	deltas strings.Builder
	final  string
}

func (a *responsesAccumulator) consumeEvent(raw jsonv1.RawMessage) error {
	if a.terminal {
		return a.err
	}
	var event struct {
		Type        string            `json:"type"`
		Response    jsonv1.RawMessage `json:"response"`
		Item        jsonv1.RawMessage `json:"item"`
		OutputIndex int               `json:"output_index"`
		Code        string            `json:"code"`
		Error       struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		// Failure classification does not depend on unrelated event fields,
		// such as a malformed output_index rejected by the assembler.
		var failure struct {
			Type     string            `json:"type"`
			Response jsonv1.RawMessage `json:"response"`
			Code     string            `json:"code"`
			Error    struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &failure) == nil {
			a.recordFailureKind(nativeResponseErrorKind(failure.Response))
			a.recordEventFailure(failure.Type, failure.Code, failure.Error.Code, failure.Error.Type)
		}
		if a.err == nil {
			a.err = providerError(ErrorResponse, 0, nil)
		}
		return a.err
	}
	// Failure classification still observes later events after an assembly
	// error. The SDK can read several events ahead in a single body.Read.
	a.recordFailureKind(nativeResponseErrorKind(event.Response))
	a.recordEventFailure(event.Type, event.Code, event.Error.Code, event.Error.Type)
	if a.err != nil {
		return a.err
	}
	switch event.Type {
	case "response.output_item.done":
		if !jsonutil.IsObject(event.Item) {
			a.err = providerError(ErrorResponse, 0, nil)
			return a.err
		}
		if a.items == nil {
			a.items = make(map[int]jsonv1.RawMessage)
		}
		a.items[event.OutputIndex] = event.Item
	case "response.refusal.delta", "response.refusal.done":
		a.consumeRefusal(event.Type, event.OutputIndex, raw)
	case "response.completed", "response.incomplete":
		var envelope map[string]jsonv1.RawMessage
		if json.Unmarshal(event.Response, &envelope) != nil || envelope == nil {
			a.err = providerError(ErrorResponse, 0, nil)
			return a.err
		}
		var status string
		if json.Unmarshal(envelope["status"], &status) != nil || "response."+status != event.Type {
			a.err = providerError(ErrorResponse, 0, nil)
			return a.err
		}
		var output []jsonv1.RawMessage
		if raw := envelope["output"]; len(raw) > 0 && json.Unmarshal(raw, &output) != nil {
			a.err = providerError(ErrorResponse, 0, nil)
			return a.err
		}
		if a.items == nil {
			a.items = make(map[int]jsonv1.RawMessage, len(output))
		}
		for index, item := range output {
			a.items[index] = item
		}
		// The items above own the terminal output. Avoid retaining its second
		// raw array representation until final response conversion.
		delete(envelope, "output")
		a.envelope = envelope
		a.terminal = true
	}
	return nil
}

func (a *responsesAccumulator) consumeRefusal(kind string, outputIndex int, raw jsonv1.RawMessage) {
	var event struct {
		ContentIndex int    `json:"content_index"`
		Delta        string `json:"delta"`
		Refusal      string `json:"refusal"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return
	}
	index := responsePartIndex{output: outputIndex, content: event.ContentIndex}
	if a.refusals == nil {
		a.refusals = make(map[responsePartIndex]*responseRefusal)
	}
	part := a.refusals[index]
	if part == nil {
		part = &responseRefusal{}
		a.refusals[index] = part
	}
	if kind == "response.refusal.done" {
		part.final = event.Refusal
	} else {
		part.deltas.WriteString(event.Delta)
	}
}

func nativeResponseErrorKind(raw jsonv1.RawMessage) ErrorKind {
	if len(raw) == 0 {
		return ""
	}
	var failure struct {
		ErrorType string `json:"error_type"`
		Error     struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &failure) != nil {
		return ""
	}
	if kind := openRouterErrorKind(failure.ErrorType); kind != "" {
		return kind
	}
	return providerFailureCodeKind(failure.Error.Code, failure.Error.Type)
}

func nativeQuotaResponse(raw jsonv1.RawMessage) bool {
	return nativeResponseErrorKind(raw) == ErrorQuota
}

func (a *responsesAccumulator) recordEventFailure(eventType string, codes ...string) {
	switch eventType {
	case "error":
		a.recordFailureKind(providerFailureCodeKind(codes...))
		fallthrough
	case "response.failed":
		a.recordFailureKind(ErrorResponse)
	}
}

func (a *responsesAccumulator) recordFailureKind(kind ErrorKind) {
	if kind == "" || a.failureKind == ErrorQuota || a.failureKind == ErrorAuthentication {
		return
	}
	if kind == ErrorQuota || kind == ErrorAuthentication || a.failureKind == "" {
		a.failureKind = kind
	}
}

func providerErrorCodeFromJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var errorBody struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
		Code string `json:"code"`
	}
	if json.Unmarshal(raw, &errorBody) != nil {
		return ""
	}
	if errorBody.Error.Code != "" {
		return errorBody.Error.Code
	}
	if errorBody.Error.Type != "" {
		return errorBody.Error.Type
	}
	return errorBody.Code
}

func (a *responsesAccumulator) response() (Response, error) {
	if a.err != nil {
		return Response{}, a.err
	}
	if !a.terminal {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	indices := make([]int, 0, len(a.items))
	for index := range a.items {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	output := make([]jsonv1.RawMessage, 0, len(a.items))
	for _, index := range indices {
		output = append(output, a.items[index])
	}
	a.envelope["output"], _ = json.Marshal(output)
	assembled, err := json.Marshal(a.envelope)
	// Repeated result reads may reuse items, but need not retain a second copy.
	delete(a.envelope, "output")
	if err != nil {
		return Response{}, err
	}
	return responseFromNative(assembled, a.streamedRefusal())
}

func (a *responsesAccumulator) streamedRefusal() string {
	if len(a.refusals) == 0 {
		return ""
	}
	indices := make([]responsePartIndex, 0, len(a.refusals))
	for index := range a.refusals {
		indices = append(indices, index)
	}
	sort.Slice(indices, func(i, j int) bool {
		left, right := indices[i], indices[j]
		if left.output == right.output {
			return left.content < right.content
		}
		return left.output < right.output
	})
	var refusal strings.Builder
	for _, index := range indices {
		part := a.refusals[index]
		if part.final != "" {
			refusal.WriteString(part.final)
		} else {
			refusal.WriteString(part.deltas.String())
		}
	}
	return refusal.String()
}
