// Package observation reconstructs one agent run from a NOE JSONL event log.
package observation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"noe/internal/events"
)

// Cursor identifies a position in one NOE execution's event stream. The
// execution ID is part of the cursor because explicit log files may be reused
// and appended to by multiple NOE processes whose sequence numbers restart.
type Cursor struct {
	ExecutionID string `json:"executionId"`
	Seq         uint64 `json:"seq"`
}

// Query describes one observation of a run in a JSONL event log.
type Query struct {
	LogPath         string  `json:"logPath"`
	RunID           string  `json:"runId"`
	Cursor          *Cursor `json:"cursor,omitempty"`
	Wait            bool    `json:"wait,omitempty"`
	IncludeThinking bool    `json:"includeThinking,omitempty"`
	IncludeOutput   bool    `json:"includeOutput,omitempty"`
}

// Usage contains accumulated provider-reported usage for a run.
type Usage struct {
	PromptTokens     int64   `json:"promptTokens,omitempty"`
	CompletionTokens int64   `json:"completionTokens,omitempty"`
	TotalTokens      int64   `json:"totalTokens,omitempty"`
	CachedTokens     int64   `json:"cachedTokens,omitempty"`
	Cost             float64 `json:"cost,omitempty"`
}

// ToolResult describes the most recently completed run_js call. Text contains
// that tool's captured stdout and stderr when output was requested.
type ToolResult struct {
	ToolCallID string `json:"toolCallId"`
	Success    bool   `json:"success"`
	ExitCode   int    `json:"exitCode"`
	Reason     string `json:"reason,omitempty"`
	Duration   int64  `json:"duration,omitempty"`
	Text       string `json:"text,omitempty"`
}

// Snapshot is the reconstructed state of one run plus its meaningful event
// delta. State fields are always derived from the full run history; Cursor
// filters only Events.
type Snapshot struct {
	AgentID        string         `json:"agentId,omitempty"`
	RunID          string         `json:"runId"`
	ParentRunID    string         `json:"parentRunId"`
	Name           string         `json:"name"`
	Model          string         `json:"model"`
	Status         string         `json:"status"`
	Phase          string         `json:"phase"`
	Action         string         `json:"action"`
	LastMessage    string         `json:"lastMessage"`
	FinalText      string         `json:"finalText,omitempty"`
	Error          string         `json:"error,omitempty"`
	LastToolResult *ToolResult    `json:"lastToolResult,omitempty"`
	Usage          *Usage         `json:"usage,omitempty"`
	UpdatedAt      string         `json:"updatedAt"`
	Cursor         Cursor         `json:"cursor"`
	Events         []events.Event `json:"events"`
}

type record struct {
	event       events.Event
	executionID string
	seq         uint64
	typeName    string
}

type activeTool struct {
	seq       uint64
	toolCall  string
	action    string
	hasAction bool
}

type toolOutput struct {
	text strings.Builder
}

type runState struct {
	snapshot    Snapshot
	phase       string
	models      map[string]uint64
	tools       map[string]activeTool
	outputs     map[string]*toolOutput
	lastTool    activeTool
	lastToolSet bool
	usageSet    bool
}

// Observe reads the log on every call so callers can reuse a cursor without
// maintaining any hidden reader state. When Wait is set, it polls until the
// selected run reaches an agent lifecycle terminal event or ctx is cancelled.
func Observe(ctx context.Context, q Query) (Snapshot, error) {
	if q.LogPath == "" {
		return Snapshot{}, errors.New("observation logPath is required")
	}
	if q.RunID == "" {
		return Snapshot{}, errors.New("observation runId is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// An unknown run has no execution yet. Its empty initial cursor can be
	// passed back unchanged when the first records become available.
	if q.Cursor != nil && q.Cursor.ExecutionID == "" && q.Cursor.Seq == 0 {
		q.Cursor = nil
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	for {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		snapshot, terminal, err := observeOnce(ctx, q)
		if err != nil {
			return Snapshot{}, err
		}
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		if !q.Wait || terminal {
			return snapshot, nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return Snapshot{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func observeOnce(ctx context.Context, q Query) (Snapshot, bool, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}
	records, err := readRecords(ctx, q.LogPath)
	if err != nil {
		return Snapshot{}, false, err
	}

	targetExecution := ""
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, false, err
		}
		if stringField(rec.event, "runId") != q.RunID {
			continue
		}
		if targetExecution != "" && targetExecution != rec.executionID {
			return Snapshot{}, false, fmt.Errorf("runId %q occurs in multiple executions in %q; the target is ambiguous", q.RunID, q.LogPath)
		}
		targetExecution = rec.executionID
	}

	if targetExecution == "" {
		next := Cursor{}
		if q.Cursor != nil {
			next = *q.Cursor
		}
		return Snapshot{
			RunID: q.RunID, Status: "unknown", Phase: "unknown", Cursor: next,
			Events: make([]events.Event, 0),
		}, false, nil
	}
	if q.Cursor != nil && q.Cursor.ExecutionID != targetExecution {
		return Snapshot{}, false, fmt.Errorf("cursor executionId %q does not match runId %q executionId %q; use the cursor returned for this run", q.Cursor.ExecutionID, q.RunID, targetExecution)
	}

	maxSeq := uint64(0)
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, false, err
		}
		if rec.executionID == targetExecution && rec.seq > maxSeq {
			maxSeq = rec.seq
		}
	}

	state := newRunState(q.RunID)
	var lastProjectedSeq uint64
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, false, err
		}
		if rec.executionID != targetExecution || stringField(rec.event, "runId") != q.RunID {
			continue
		}
		state.apply(rec, q.IncludeOutput)
		if rec.seq > cursorSeq(q.Cursor) {
			if projected := projectEvent(rec.event, q); projected != nil {
				appendProjected(&state.snapshot.Events, projected, rec.seq, &lastProjectedSeq)
			}
		}
	}

	state.snapshot.Cursor = Cursor{ExecutionID: targetExecution, Seq: maxSeq}
	state.snapshot.Phase = state.currentPhase()
	if state.snapshot.Status == "completed" || state.snapshot.Status == "failed" || state.snapshot.Status == "cancelled" {
		state.snapshot.Phase = "settled"
	}
	state.snapshot.Action = state.currentAction()
	if state.snapshot.Events == nil {
		state.snapshot.Events = make([]events.Event, 0)
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}
	return state.snapshot, isTerminalStatus(state.snapshot.Status), nil
}

func readRecords(ctx context.Context, path string) ([]record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open observation log %q: %w", path, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat observation log %q: %w", path, err)
	}
	cutoff := info.Size()
	reader := bufio.NewReaderSize(contextReader{ctx: ctx, reader: io.LimitReader(file, cutoff)}, 32*1024)
	records := make([]record, 0)
	lineNumber := 0
	for {
		line, complete, readErr := readCompleteLine(ctx, reader)
		if readErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if readErr != io.EOF {
				return nil, fmt.Errorf("read observation log %q: %w", path, readErr)
			}
			// A writer may be between writes. Ignore only the unterminated tail;
			// the next query will include it once its newline is visible.
			break
		}
		if !complete {
			break
		}
		lineNumber++
		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		rec, parseErr := parseRecord(ctx, line)
		if parseErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("invalid observation log %q at complete line %d: %w", path, lineNumber, parseErr)
		}
		records = append(records, rec)
	}
	return records, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > 32*1024 {
		p = p[:32*1024]
	}
	return r.reader.Read(p)
}

func readCompleteLine(ctx context.Context, reader *bufio.Reader) ([]byte, bool, error) {
	var line []byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		part, err := reader.ReadSlice('\n')
		line = append(line, part...)
		switch err {
		case nil:
			return line, true, nil
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			// Do not parse a record until its terminating newline has arrived.
			return nil, false, nil
		default:
			return nil, false, err
		}
	}
}

func parseRecord(ctx context.Context, line []byte) (record, error) {
	if err := ctx.Err(); err != nil {
		return record{}, err
	}
	decoder := json.NewDecoder(contextReader{ctx: ctx, reader: bytes.NewReader(line)})
	decoder.UseNumber()
	var event events.Event
	if err := decoder.Decode(&event); err != nil {
		return record{}, err
	}
	if err := ctx.Err(); err != nil {
		return record{}, err
	}
	if event == nil {
		return record{}, errors.New("expected a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return record{}, errors.New("multiple JSON values in one line")
		}
		return record{}, err
	}
	if err := ctx.Err(); err != nil {
		return record{}, err
	}
	executionID, ok := event["executionId"].(string)
	if !ok || executionID == "" {
		return record{}, errors.New("missing or invalid executionId")
	}
	typeName, ok := event["type"].(string)
	if !ok || typeName == "" {
		return record{}, errors.New("missing or invalid type")
	}
	seq, ok := uintField(event["seq"])
	if !ok || seq == 0 {
		return record{}, errors.New("missing or invalid seq")
	}
	return record{event: event, executionID: executionID, seq: seq, typeName: typeName}, nil
}

func newRunState(runID string) *runState {
	return &runState{
		snapshot: Snapshot{
			RunID: runID, Status: "unknown", Phase: "unknown", Events: make([]events.Event, 0),
		},
		models: make(map[string]uint64), tools: make(map[string]activeTool),
		outputs: make(map[string]*toolOutput),
	}
}

func (s *runState) apply(rec record, includeOutput bool) {
	event := rec.event
	typeName := rec.typeName
	if value := stringField(event, "agentId"); value != "" {
		s.snapshot.AgentID = value
	}
	if timeText := stringField(event, "time"); timeText != "" {
		s.snapshot.UpdatedAt = timeText
	}
	if value := stringField(event, "parentRunId"); value != "" || typeName == "agent.started" {
		s.snapshot.ParentRunID = value
	}
	if value := stringField(event, "name"); value != "" {
		s.snapshot.Name = value
	}
	if value := stringField(event, "model"); value != "" {
		s.snapshot.Model = value
	}

	switch typeName {
	case "agent.started":
		s.snapshot.Status = "running"
		s.phase = "starting"
	case "agent.completed":
		s.snapshot.Status = "completed"
	case "agent.failed":
		s.snapshot.Status = "failed"
		s.snapshot.Error = stringField(event, "error")
	case "agent.cancelled":
		s.snapshot.Status = "cancelled"
	case "agent.message":
		s.snapshot.LastMessage = stringField(event, "text")
	case "agent.final":
		s.snapshot.FinalText = stringField(event, "text")
	case "model.started":
		requestID := stringField(event, "requestId")
		s.models[requestID] = rec.seq
		s.phase = "between_steps"
	case "model.finished", "model.failed":
		delete(s.models, stringField(event, "requestId"))
		s.phase = "between_steps"
		if typeName == "model.finished" {
			s.applyUsage(event["usage"])
		}
	case "run_js.started":
		toolCallID := stringField(event, "toolCallId")
		key := toolKey(toolCallID, rec.seq)
		action, hasAction := event["text"].(string)
		active := activeTool{seq: rec.seq, toolCall: toolCallID, action: action, hasAction: hasAction}
		s.tools[key] = active
		s.lastTool, s.lastToolSet = active, true
		s.phase = "between_steps"
	case "stdout", "stderr":
		if includeOutput && stringField(event, "source") == "run_js" {
			toolCallID := stringField(event, "toolCallId")
			if toolCallID != "" {
				out := s.outputs[toolCallID]
				if out == nil {
					out = &toolOutput{}
					s.outputs[toolCallID] = out
				}
				out.text.WriteString(stringField(event, "text"))
			}
		}
	case "run_js.finished":
		toolCallID := stringField(event, "toolCallId")
		delete(s.tools, toolKey(toolCallID, 0))
		// A finished event may be the only record for a tool if its start was
		// outside the observed file prefix; remove any matching active entry.
		for key, active := range s.tools {
			if active.toolCall == toolCallID {
				delete(s.tools, key)
			}
		}
		result := &ToolResult{
			ToolCallID: toolCallID,
			Success:    boolField(event, "success"),
			ExitCode:   intField(event["exitCode"]),
			Reason:     stringField(event, "reason"),
			Duration:   intField64(event["duration"]),
		}
		if includeOutput {
			if output := s.outputs[toolCallID]; output != nil {
				result.Text = output.text.String()
			}
		}
		s.snapshot.LastToolResult = result
		s.phase = "between_steps"
	case "thinking", "thinking_summary", "message_delta":
		// Stream fragments do not change the run's phase.
	}
}

func (s *runState) applyUsage(raw any) {
	values, ok := raw.(map[string]any)
	if !ok {
		return
	}
	if s.snapshot.Usage == nil {
		s.snapshot.Usage = &Usage{}
	}
	usage := s.snapshot.Usage
	addInt := func(key string, target *int64) {
		if value, ok := int64Field(values[key]); ok {
			*target += value
			s.usageSet = true
		}
	}
	addInt("promptTokens", &usage.PromptTokens)
	addInt("completionTokens", &usage.CompletionTokens)
	addInt("totalTokens", &usage.TotalTokens)
	addInt("cachedTokens", &usage.CachedTokens)
	if value, ok := float64Field(values["cost"]); ok {
		usage.Cost += value
		s.usageSet = true
	}
	if !s.usageSet {
		s.snapshot.Usage = nil
	}
}

func (s *runState) currentPhase() string {
	if isTerminalStatus(s.snapshot.Status) {
		return "settled"
	}
	if len(s.tools) > 0 {
		return "run_js"
	}
	if len(s.models) > 0 {
		return "model"
	}
	if s.phase != "" {
		return s.phase
	}
	return "unknown"
}

func (s *runState) currentAction() string {
	if len(s.tools) > 0 {
		var current activeTool
		found := false
		for _, active := range s.tools {
			if !found || active.seq > current.seq {
				current, found = active, true
			}
		}
		if found && current.hasAction {
			return current.action
		}
		return ""
	}
	if s.lastToolSet && s.lastTool.hasAction {
		return s.lastTool.action
	}
	return ""
}

func projectEvent(event events.Event, q Query) events.Event {
	typeName := stringField(event, "type")
	switch typeName {
	case "agent.started", "agent.completed", "agent.failed", "agent.cancelled",
		"agent.message", "agent.final", "model.started", "model.finished", "model.failed",
		"run_js.started", "run_js.finished", "decision.started", "decision.completed", "decision.failed", "decision.cancelled":
		return cloneEvent(event)
	case "thinking", "thinking_summary":
		if q.IncludeThinking {
			return cloneEvent(event)
		}
	case "stdout", "stderr":
		if q.IncludeOutput {
			return cloneEvent(event)
		}
	}
	return nil
}

func appendProjected(eventsOut *[]events.Event, event events.Event, seq uint64, previousSeq *uint64) {
	if len(*eventsOut) > 0 && seq == *previousSeq+1 && isThinkingType(stringField(event, "type")) {
		previous := (*eventsOut)[len(*eventsOut)-1]
		if stringField(previous, "type") == stringField(event, "type") &&
			stringField(previous, "requestId") == stringField(event, "requestId") {
			previous["text"] = stringField(previous, "text") + stringField(event, "text")
			*previousSeq = seq
			return
		}
	}
	*eventsOut = append(*eventsOut, event)
	*previousSeq = seq
}

func cloneEvent(event events.Event) events.Event {
	copy := make(events.Event, len(event))
	for key, value := range event {
		copy[key] = value
	}
	return copy
}

func isThinkingType(typeName string) bool {
	return typeName == "thinking" || typeName == "thinking_summary"
}

func isTerminalStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}

func cursorSeq(cursor *Cursor) uint64 {
	if cursor == nil {
		return 0
	}
	return cursor.Seq
}

func toolKey(toolCallID string, seq uint64) string {
	if toolCallID != "" {
		return toolCallID
	}
	return "#" + strconv.FormatUint(seq, 10)
}

func stringField(event events.Event, key string) string {
	value, _ := event[key].(string)
	return value
}

func boolField(event events.Event, key string) bool {
	value, _ := event[key].(bool)
	return value
}

func uintField(value any) (uint64, bool) {
	if number, ok := value.(json.Number); ok {
		parsed, err := strconv.ParseUint(number.String(), 10, 64)
		return parsed, err == nil
	}
	switch number := value.(type) {
	case uint64:
		return number, true
	case uint:
		return uint64(number), true
	case int64:
		if number >= 0 {
			return uint64(number), true
		}
	case int:
		if number >= 0 {
			return uint64(number), true
		}
	case float64:
		if number >= 0 && number <= math.MaxUint64 && math.Trunc(number) == number {
			return uint64(number), true
		}
	}
	return 0, false
}

func int64Field(value any) (int64, bool) {
	if number, ok := value.(json.Number); ok {
		parsed, err := strconv.ParseInt(number.String(), 10, 64)
		return parsed, err == nil
	}
	switch number := value.(type) {
	case int64:
		return number, true
	case int:
		return int64(number), true
	case uint64:
		if number <= math.MaxInt64 {
			return int64(number), true
		}
	case float64:
		if number >= math.MinInt64 && number <= math.MaxInt64 && math.Trunc(number) == number {
			return int64(number), true
		}
	}
	return 0, false
}

func intField(value any) int {
	number, ok := int64Field(value)
	if !ok || int64(int(number)) != number {
		return 0
	}
	return int(number)
}

func intField64(value any) int64 {
	number, _ := int64Field(value)
	return number
}

func float64Field(value any) (float64, bool) {
	if number, ok := value.(json.Number); ok {
		parsed, err := strconv.ParseFloat(number.String(), 64)
		return parsed, err == nil
	}
	if number, ok := value.(float64); ok {
		return number, true
	}
	if number, ok := value.(int64); ok {
		return float64(number), true
	}
	if number, ok := value.(int); ok {
		return float64(number), true
	}
	return 0, false
}
