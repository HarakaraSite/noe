package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

func imageErrorCode(err error) string {
	var imageErr *imageapi.Error
	if errors.As(err, &imageErr) {
		return imageErr.Code
	}
	return ""
}

func TestOperationReadImageUsesSelectedReaderAndKeepsPrivateSnapshot(t *testing.T) {
	h, _, child, run := imageRunFixture(t)
	content := testPNG(t)
	original := append([]byte(nil), content...)
	var eventsSeen []events.Event
	h.opts.Observe = func(event events.Event) { eventsSeen = append(eventsSeen, event) }
	const virtualPath = "virtual:/assets/fixture.png"
	metadata, err := child.ReadImage(context.Background(), virtualPath, "low", 2, func(ctx context.Context, path string) ([]byte, error) {
		if path != virtualPath {
			t.Fatalf("reader path=%q", path)
		}
		if ctx == nil || ctx.Err() != nil {
			t.Fatalf("reader context=%v", ctx)
		}
		return content, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ImageID == "" || metadata.Path != virtualPath || metadata.Detail != "low" || metadata.MIMEType != "image/png" {
		t.Fatalf("image metadata=%+v", metadata)
	}
	content[0] ^= 0xff
	h.mu.Lock()
	tool := run.modelStep.toolCalls["tool"]
	stored := append([]byte(nil), tool.rawImages[0].Content...)
	storedOrdinal := tool.rawImages[0].Ordinal
	h.mu.Unlock()
	if !bytes.Equal(stored, original) || storedOrdinal != 2 {
		t.Fatal("image bytes were not retained as an ordered Go-owned snapshot")
	}
	if len(eventsSeen) != 1 || eventsSeen[0]["type"] != "image.read" || eventsSeen[0]["image"] != metadata {
		t.Fatalf("image read events=%+v", eventsSeen)
	}
}

func TestOperationReadImageChecksOwnerBeforeFilesystemAvailability(t *testing.T) {
	_, root, child, _ := imageRunFixture(t)
	_, err := root.ReadImage(context.Background(), "image.png", "auto", 1, nil)
	if imageErrorCode(err) != "input" {
		t.Fatalf("unowned read error=%v code=%q; want owner rejection before filesystem check", err, imageErrorCode(err))
	}
	_, err = child.ReadImage(context.Background(), "image.png", "auto", 1, nil)
	if imageErrorCode(err) != "filesystem" {
		t.Fatalf("owned read with disabled filesystem error=%v code=%q", err, imageErrorCode(err))
	}
	_, err = child.ReadImage(context.Background(), "image.png", "auto", 1, func(context.Context, string) ([]byte, error) {
		t.Fatal("duplicate ordinal invoked the reader")
		return nil, nil
	})
	if imageErrorCode(err) != "input" {
		t.Fatalf("duplicate ordinal error=%v code=%q", err, imageErrorCode(err))
	}
}

func TestOperationReadImageCloseCancelsAndJoinsReader(t *testing.T) {
	h, _, child, run := imageRunFixture(t)
	content := testPNG(t)
	readerEntered := make(chan struct{})
	readerCanceled := make(chan struct{})
	releaseReader := make(chan struct{})
	var releaseOnce sync.Once
	readDone := make(chan error, 1)
	go func() {
		_, err := child.ReadImage(context.Background(), "virtual:image.png", "auto", 1, func(ctx context.Context, _ string) ([]byte, error) {
			close(readerEntered)
			<-ctx.Done()
			close(readerCanceled)
			<-releaseReader
			return content, nil
		})
		readDone <- err
	}()
	defer func() {
		releaseOnce.Do(func() { close(releaseReader) })
		h.Close()
	}()
	select {
	case <-readerEntered:
	case <-time.After(time.Second):
		t.Fatal("selected filesystem reader was not called")
	}
	closeDone := make(chan struct{})
	go func() { child.Close(); close(closeDone) }()
	select {
	case <-readerCanceled:
	case <-time.After(time.Second):
		t.Fatal("operation close did not cancel the image reader")
	}
	select {
	case <-closeDone:
		t.Fatal("operation close returned before the accepted reader joined")
	case <-time.After(30 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(releaseReader) })
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("operation close did not finish after reader release")
	}
	if err := <-readDone; imageErrorCode(err) != "cancelled" {
		t.Fatalf("read error=%v code=%q", err, imageErrorCode(err))
	}
	h.mu.Lock()
	stored := len(run.modelStep.toolCalls["tool"].rawImages)
	h.mu.Unlock()
	if stored != 0 {
		t.Fatal("late image read attached bytes after scope close")
	}
}

func TestOperationObserveImageSaveDoesNotDoubleCountGeneration(t *testing.T) {
	var seen []events.Event
	h := NewHost(Options{Observe: func(event events.Event) { seen = append(seen, event) }})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	group := &executionGroup{id: "image-save-group"}
	group.counters.admit("providerCalls", "image")
	group.usage.add(summarizeUsage(json.RawMessage(`{"total_tokens":7,"cost":0.01}`)))
	op.group = group
	op.causeActivityIDs = []string{"generation-activity"}
	op.activityOrigin = activityOrigin{Kind: "tool", RunID: "owned-run", ToolCallID: "tool-call", ToolName: "run_js"}
	generated := model.GeneratedImage{
		RequestID: "provider-request", ResponseModel: "fixture-image", Usage: json.RawMessage(`{"total_tokens":7,"cost":0.01}`),
	}
	metadata := imageapi.Metadata{Path: "output.png", MIMEType: "image/png", Bytes: 12, Width: 2, Height: 3}
	op.ObserveImageSave("output.png", generated, metadata, nil)
	op.ObserveImageSave("output.png", generated, imageapi.Metadata{}, &imageapi.Error{Code: "filesystem", Message: "write failed"})
	if len(seen) != 2 || seen[0]["type"] != "image.saved" || seen[1]["type"] != "image.save_failed" {
		t.Fatalf("save events=%+v", seen)
	}
	for _, event := range seen {
		if event["groupId"] != group.id || event["origin"] != op.activityOrigin || event["causedBy"].([]string)[0] != "generation-activity" || event["providerRequestId"] != generated.RequestID {
			t.Fatalf("save event lost request ownership: %+v", event)
		}
		encoded, err := json.Marshal(event)
		if err != nil || !bytes.Contains(encoded, []byte(`"usage":{"total_tokens":7,"cost":0.01}`)) {
			t.Fatalf("save event usage=%s err=%v", encoded, err)
		}
	}
	h.mu.Lock()
	count, usage := group.counters.ProviderCalls, group.usage.TotalTokens
	h.mu.Unlock()
	if count != 1 || usage == nil || *usage != 7 {
		t.Fatalf("save observer counted provider generation again: calls=%d usage=%+v", count, usage)
	}
}
