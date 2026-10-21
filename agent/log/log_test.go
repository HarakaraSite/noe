package log_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	agentlog "forge.harakara.site/littleisland/noe/v2/agent/log"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const sample = `{"executionId":"exec","seq":1,"type":"agent.started","runId":"r","name":"sample","model":"fixture"}
{"executionId":"exec","seq":2,"type":"agent.message","runId":"r","text":"earlier progress"}
{"executionId":"exec","seq":3,"type":"model.started","runId":"r","requestId":"request"}
{"executionId":"exec","seq":4,"type":"model.finished","runId":"r","requestId":"request","usage":{"promptTokens":100,"cachedTokens":90,"completionTokens":2,"totalTokens":102}}
{"executionId":"exec","seq":5,"type":"agent.final","runId":"r","text":"answer"}
{"executionId":"exec","seq":6,"type":"agent.completed","runId":"r","status":"completed"}
`

func TestAnalyzeCompleteSnapshotRetainsStateAcrossCursorAndIncompleteTail(t *testing.T) {
	query := agentlog.Query{RunID: "r", Cursor: &agentlog.Cursor{ExecutionID: "exec", Seq: 4}}
	got, err := agentlog.AnalyzeBytes(context.Background(), []byte(sample+`{"incomplete"`), query)
	if err != nil || got.Status != "completed" || got.LastMessage != "earlier progress" || got.FinalText != "answer" || len(got.Events) != 2 || got.Usage == nil || got.Usage.CachedTokens != 90 || got.Cursor.Seq != 6 {
		t.Fatalf("snapshot=%+v err=%v", got, err)
	}
	query.Cursor = &got.Cursor
	again, err := agentlog.Analyze(context.Background(), strings.NewReader(sample), query)
	if err != nil || len(again.Events) != 0 || !reflect.DeepEqual(again.Usage, got.Usage) || again.FinalText != got.FinalText {
		t.Fatalf("repeated snapshot=%+v err=%v", again, err)
	}
	incomplete, err := agentlog.AnalyzeBytes(context.Background(), []byte(strings.TrimSuffix(sample, "\n")), agentlog.Query{RunID: "r"})
	if err != nil || incomplete.Status != "running" || incomplete.Cursor.Seq != 5 {
		t.Fatalf("tail snapshot=%+v err=%v", incomplete, err)
	}
}

type cancelReader struct {
	reader *bytes.Reader
	cancel context.CancelFunc
	closed bool
}

func (r *cancelReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.cancel()
	return n, err
}
func (r *cancelReader) Close() error { r.closed = true; return nil }
func TestAnalyzeHonorsCancellationDuringInputAndBorrowsReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &cancelReader{reader: bytes.NewReader([]byte(sample)), cancel: cancel}
	if _, err := agentlog.Analyze(ctx, reader, agentlog.Query{RunID: "r"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("analysis cancellation=%v", err)
	}
	if reader.closed {
		t.Fatal("analysis closed caller reader")
	}
}
func TestAnalyzeExistingSavedLogFormatWithoutHost(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "observation", "testdata", "real-run.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := agentlog.AnalyzeBytes(context.Background(), data, agentlog.Query{RunID: "423a2834915a42cb0a5fe2533117e0354010ad4ae4e8846f", IncludeThinking: true, IncludeOutput: true})
	if err != nil || snapshot.Status != "completed" || snapshot.Cursor.ExecutionID != "7ca3f7070d98c10f" || snapshot.FinalText == "" {
		t.Fatalf("existing log=%+v err=%v", snapshot, err)
	}
}
func TestLogBindingAnalyzesStringAndUint8ArrayWithoutHostOrFilesystem(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[log consumer]", Virtual: true, Module: true, Code: `
  import {analyze_log} from "noe:agent-log";
  for(const input of [logInput,new TextEncoder().encode(logInput)]){
   const s=analyze_log(input,{runId:"r",cursor:{executionId:"exec",seq:4}});
   if(s.status!=="completed"||s.lastMessage!=="earlier progress"||s.finalText!=="answer"||s.events.length!==2||s.usage.cachedTokens!==90)throw Error("snapshot");
   const repeated=analyze_log(input,{runId:"r",cursor:s.cursor});
   if(repeated.events.length!==0||repeated.finalText!=="answer")throw Error("cursor lost full state");
  }
  let rejected=false;try{analyze_log(logInput,{runId:"r",logPath:"unused"})}catch(e){rejected=e instanceof TypeError}if(!rejected)throw Error("file target accepted");
 `}, stdlib.Profile{Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
		binding, err := agentlog.Bind(ctx, b)
		if err != nil {
			return nil, err
		}
		if err := b.Runtime().Set("logInput", sample); err != nil {
			binding.Close()
			return nil, err
		}
		return binding.Close, nil
	}})
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
}
