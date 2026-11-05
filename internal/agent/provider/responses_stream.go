package provider

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"sort"
)

// responsesAccumulator keeps completed items and the terminal envelope, not
// the stream's delta history. Some providers put full items only in
// output_item.done, with an empty response.output in response.completed.
type responsesAccumulator struct {
	items     map[int]jsonv1.RawMessage
	envelope  map[string]jsonv1.RawMessage
	completed bool
	quota     bool
	err       error
}

func (a *responsesAccumulator) consumeEvent(raw jsonv1.RawMessage) error {
	var event struct {
		Type        string            `json:"type"`
		Response    jsonv1.RawMessage `json:"response"`
		Item        jsonv1.RawMessage `json:"item"`
		OutputIndex int               `json:"output_index"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		// Failure classification does not depend on unrelated event fields,
		// such as a malformed output_index rejected by the assembler.
		var failure struct {
			Response jsonv1.RawMessage `json:"response"`
		}
		if json.Unmarshal(raw, &failure) == nil {
			a.quota = a.quota || nativeQuotaResponse(failure.Response)
		}
		if !a.completed && a.err == nil {
			a.err = providerError(ErrorResponse, 0, nil)
		}
		return a.err
	}
	// Failure classification still observes later events after an assembly
	// error. The SDK can read several events ahead in a single body.Read.
	a.quota = a.quota || nativeQuotaResponse(event.Response)
	if a.completed || a.err != nil {
		return a.err
	}
	switch event.Type {
	case "response.output_item.done":
		if !isJSONObject(event.Item) {
			a.err = providerError(ErrorResponse, 0, nil)
			return a.err
		}
		if a.items == nil {
			a.items = make(map[int]jsonv1.RawMessage)
		}
		a.items[event.OutputIndex] = event.Item
	case "response.completed":
		var envelope map[string]jsonv1.RawMessage
		if json.Unmarshal(event.Response, &envelope) != nil || envelope == nil {
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
		a.completed = true
	}
	return nil
}

func nativeQuotaResponse(raw jsonv1.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var failure struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &failure) != nil {
		return false
	}
	return failure.Error.Code == "subscription_sharing_usage_limit_exceeded" || failure.Error.Code == "subscription_sharing_usage_unavailable"
}

func (a *responsesAccumulator) response() (Response, error) {
	if a.err != nil {
		return Response{}, a.err
	}
	if !a.completed {
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
	return responseFromNative(assembled)
}
