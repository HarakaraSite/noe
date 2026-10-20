package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

// conversationSnapshot is the current pruned transcript owned by one agent
// instance. It crosses only the authenticated private task RPC.
type conversationSnapshot struct {
	Revision int64               `json:"revision"`
	Sequence int64               `json:"sequence"`
	Entries  []conversationEntry `json:"entries"`
}

type conversationEntry struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Messages []json.RawMessage `json:"messages"`
}

type conversationCheckpointRequest struct {
	RunID    string                `json:"runId"`
	Snapshot *conversationSnapshot `json:"snapshot"`
}

func (b *broker) handleConversationCheckpoint(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request conversationCheckpointRequest
	if decodeRequest(r, &request) != nil || request.RunID == "" || request.Snapshot == nil {
		writeRPCError(w, http.StatusBadRequest, "request", "runId and snapshot are required")
		return
	}
	if err := validateConversationSnapshot(request.Snapshot); err != nil {
		writeRPCError(w, http.StatusBadRequest, "conversation", err.Error())
		return
	}
	h := b.host
	h.mu.Lock()
	run := h.runs[request.RunID]
	if op == nil || run == nil || run.operation != op || (run.instance != nil && (op.task == nil || run.instance != op.task.instance)) ||
		run.ctx.Err() != nil || run.interrupting || op.closed || h.closed || !h.executionGroupOpenLocked(run.group) {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "scope", "conversation checkpoint is outside the active managed task")
		return
	}
	step := run.modelStep
	if step == nil || !step.responseComplete || !step.finished || len(step.toolCalls) == 0 || run.context.pendingPlan != nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "conversation", "only a complete tool exchange can be checkpointed")
		return
	}
	instance := run.instance
	var appendErr error
	if instance != nil {
		appendErr = validateConversationAppend(instance.conversation, request.Snapshot, 1, "block")
	}
	if err := appendErr; err != nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "conversation", err.Error())
		return
	}
	entry := request.Snapshot.Entries[len(request.Snapshot.Entries)-1]
	if err := validateCompletedToolBlock(entry, step); err != nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "conversation", err.Error())
		return
	}
	if err := commitStepImagesLocked(run, entry); err != nil {
		h.mu.Unlock()
		writeRPCError(w, 409, "image", err.Error())
		return
	}
	if instance != nil {
		instance.conversation = cloneConversationSnapshot(request.Snapshot)
	}
	h.mu.Unlock()
	writeJSON(w, map[string]bool{"committed": true})
}

func validateCompletedToolBlock(entry conversationEntry, step *modelStepState) error {
	var assistant struct {
		ToolCalls []struct {
			ID string `json:"id"`
		} `json:"tool_calls"`
	}
	if json.Unmarshal(conversationResponseMessages(entry.Messages)[0], &assistant) != nil || len(assistant.ToolCalls) != len(step.toolCallOrder) {
		return errors.New("tool checkpoint does not match the completed model response")
	}
	for index, call := range assistant.ToolCalls {
		if call.ID != step.toolCallOrder[index] {
			return errors.New("tool checkpoint call order does not match the completed model response")
		}
		state := step.toolCalls[call.ID]
		if state == nil || !state.resultRecorded {
			return errors.New("tool checkpoint contains an uncompleted tool call")
		}
	}
	return nil
}

func validateFinalConversationCandidate(current, next *conversationSnapshot, answer string, alreadyStored bool) error {
	if err := validateConversationSnapshot(next); err != nil {
		return err
	}
	if !alreadyStored {
		if err := validateConversationAppend(current, next, 1, "block"); err != nil {
			return err
		}
	}
	if len(next.Entries) == 0 {
		return errors.New("final conversation candidate is missing")
	}
	entry := next.Entries[len(next.Entries)-1]
	if entry.Kind != "block" || len(conversationResponseMessages(entry.Messages)) != 1 {
		return errors.New("final conversation candidate must contain one assistant message")
	}
	if err := validateConversationBlock(entry.Messages); err != nil {
		return err
	}
	text, err := assistantFinalText(conversationResponseMessages(entry.Messages)[0])
	if err != nil || strings.TrimSpace(text) == "" || text != answer {
		return errors.New("final answer does not match the assistant message")
	}
	return nil
}

func assistantFinalText(raw json.RawMessage) (string, error) {
	var message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &message) != nil || message.Role != "assistant" {
		return "", errors.New("final conversation candidate must be an assistant message")
	}
	var content string
	if json.Unmarshal(message.Content, &content) == nil {
		return content, nil
	}
	var parts []json.RawMessage
	if json.Unmarshal(message.Content, &parts) != nil {
		return "", nil
	}
	var text []string
	for _, rawPart := range parts {
		var part struct {
			Type string          `json:"type"`
			Text json.RawMessage `json:"text"`
		}
		if json.Unmarshal(rawPart, &part) != nil || part.Type != "text" {
			continue
		}
		var value string
		if json.Unmarshal(part.Text, &value) == nil {
			text = append(text, value)
		}
	}
	return strings.Join(text, "\n"), nil
}

func initialConversation(system, instruction string) (*conversationSnapshot, error) {
	snapshot := &conversationSnapshot{}
	if err := snapshot.append("protected", map[string]string{"role": "system", "content": system}); err != nil {
		return nil, err
	}
	if err := snapshot.append("protected", map[string]string{"role": "user", "content": instruction}); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func composeSystemMessage(userInstructions, explicit string) string {
	builtin := Instructions()
	if userInstructions != "" {
		parts := []string{builtin, userInstructions}
		if explicit != "" {
			parts = append(parts, explicit)
		}
		return strings.Join(parts, "\n\n")
	}
	if explicit != "" {
		return builtin + "\n" + explicit
	}
	return builtin
}

func (s *conversationSnapshot) append(kind string, message any) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return s.appendRaw(kind, []json.RawMessage{encoded})
}

func (s *conversationSnapshot) appendRaw(kind string, messages []json.RawMessage) error {
	if s == nil {
		return errors.New("conversation snapshot is missing")
	}
	if s.Sequence >= maxSafeContextBudget || s.Revision >= maxSafeContextBudget {
		return errors.New("conversation snapshot sequence is exhausted")
	}
	s.Sequence++
	s.Revision++
	copyMessages := make([]json.RawMessage, len(messages))
	for index, message := range messages {
		copyMessages[index] = append(json.RawMessage(nil), message...)
	}
	s.Entries = append(s.Entries, conversationEntry{
		ID: fmt.Sprintf("history-%d", s.Sequence), Kind: kind, Messages: copyMessages,
	})
	return nil
}

func cloneConversationSnapshot(snapshot *conversationSnapshot) *conversationSnapshot {
	if snapshot == nil {
		return nil
	}
	copy := &conversationSnapshot{Revision: snapshot.Revision, Sequence: snapshot.Sequence}
	if snapshot.Entries != nil {
		copy.Entries = make([]conversationEntry, len(snapshot.Entries))
		for index, entry := range snapshot.Entries {
			copy.Entries[index] = conversationEntry{ID: entry.ID, Kind: entry.Kind}
			if entry.Messages != nil {
				copy.Entries[index].Messages = make([]json.RawMessage, len(entry.Messages))
				for messageIndex, message := range entry.Messages {
					copy.Entries[index].Messages[messageIndex] = append(json.RawMessage(nil), message...)
				}
			}
		}
	}
	return copy
}

func (s *conversationSnapshot) appendProtected(instruction string) error {
	return s.append("protected", map[string]string{"role": "user", "content": instruction})
}

func (s *conversationSnapshot) messages() []json.RawMessage {
	if s == nil {
		return nil
	}
	var messages []json.RawMessage
	for _, entry := range s.Entries {
		for _, message := range entry.Messages {
			messages = append(messages, append(json.RawMessage(nil), message...))
		}
	}
	return messages
}

func (s *conversationSnapshot) layout() *contextHistoryLayout {
	if s == nil {
		return nil
	}
	layout := &contextHistoryLayout{Revision: s.Revision, Entries: make([]contextHistoryEntry, 0, len(s.Entries))}
	for _, entry := range s.Entries {
		layout.Entries = append(layout.Entries, contextHistoryEntry{ID: entry.ID, Kind: entry.Kind, Count: len(entry.Messages)})
	}
	return layout
}

func validateConversationSnapshot(snapshot *conversationSnapshot) error {
	if snapshot == nil || snapshot.Revision < 2 || snapshot.Sequence < 2 || snapshot.Sequence > snapshot.Revision || len(snapshot.Entries) < 2 {
		return errors.New("invalid conversation snapshot")
	}
	seen := make(map[string]struct{}, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		if entry.ID == "" || len(entry.Messages) == 0 || (entry.Kind != "protected" && entry.Kind != "block") {
			return errors.New("invalid conversation entry")
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			return errors.New("duplicate conversation entry ID")
		}
		seen[entry.ID] = struct{}{}
		for _, message := range entry.Messages {
			if !isJSONObject(message) {
				return errors.New("invalid conversation message")
			}
		}
		if entry.Kind == "protected" {
			if len(entry.Messages) != 1 {
				return errors.New("protected conversation entries must contain one message")
			}
			continue
		}
		if err := validateConversationBlock(entry.Messages); err != nil {
			return err
		}
	}
	return nil
}

// A status describes this turn's runtime state. Retain it with the response so
// replay preserves the sent prefix and trimming removes both together. Older
// snapshots without a status remain valid.
func conversationResponseMessages(messages []json.RawMessage) []json.RawMessage {
	if len(messages) > 0 {
		var first struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(messages[0], &first) == nil && first.Role == "user" && strings.HasPrefix(first.Content, "NOE_CONTEXT_STATUS\n") {
			return messages[1:]
		}
	}
	return messages
}

func validateConversationBlock(messages []json.RawMessage) error {
	messages = conversationResponseMessages(messages)
	if len(messages) == 0 {
		return errors.New("conversation block is missing its assistant message")
	}
	var assistant struct {
		Role      string `json:"role"`
		ToolCalls []struct {
			ID string `json:"id"`
		} `json:"tool_calls"`
	}
	if json.Unmarshal(messages[0], &assistant) != nil || assistant.Role != "assistant" {
		return errors.New("conversation blocks must start with an assistant message")
	}
	expected := len(assistant.ToolCalls) + 1
	if len(messages) == expected+1 && len(assistant.ToolCalls) > 0 && imageAttachmentMessage(messages[len(messages)-1]) {
		expected++
	}
	if len(messages) != expected {
		return errors.New("conversation block is incomplete")
	}
	seen := make(map[string]struct{}, len(assistant.ToolCalls))
	for index, call := range assistant.ToolCalls {
		if strings.TrimSpace(call.ID) == "" {
			return errors.New("conversation block has an invalid tool call ID")
		}
		if _, duplicate := seen[call.ID]; duplicate {
			return errors.New("conversation block has a duplicate tool call ID")
		}
		seen[call.ID] = struct{}{}
		var tool struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
		}
		if json.Unmarshal(messages[index+1], &tool) != nil || tool.Role != "tool" || tool.ToolCallID != call.ID {
			return errors.New("conversation block tool results do not match their calls")
		}
	}
	return nil
}

func validateConversationSnapshotMatches(snapshot *conversationSnapshot, messages []json.RawMessage, layout *contextHistoryLayout) error {
	if err := validateConversationSnapshot(snapshot); err != nil {
		return err
	}
	if layout == nil || layout.Revision != snapshot.Revision || len(layout.Entries) != len(snapshot.Entries) {
		return errors.New("conversation snapshot layout does not match")
	}
	for index, entry := range snapshot.Entries {
		layoutEntry := layout.Entries[index]
		if layoutEntry.ID != entry.ID || layoutEntry.Kind != entry.Kind || layoutEntry.Count != len(entry.Messages) {
			return errors.New("conversation snapshot layout does not match")
		}
	}
	flattened := snapshot.messages()
	if len(messages) != len(flattened) {
		return errors.New("conversation snapshot messages do not match")
	}
	for index := range messages {
		if !conversationJSONEqual(messages[index], flattened[index]) {
			return errors.New("conversation snapshot messages do not match")
		}
	}
	return nil
}

func validateConversationAppend(current, next *conversationSnapshot, count int, kind string) error {
	if err := validateConversationSnapshot(current); err != nil {
		return err
	}
	if err := validateConversationSnapshot(next); err != nil {
		return err
	}
	if count < 0 || next.Revision != current.Revision+int64(count) || next.Sequence != current.Sequence+int64(count) || len(next.Entries) != len(current.Entries)+count {
		return errors.New("conversation snapshot is stale or has an invalid revision")
	}
	for index, oldEntry := range current.Entries {
		if !conversationEntryEqual(oldEntry, next.Entries[index]) {
			return errors.New("conversation snapshot does not extend the current transcript")
		}
	}
	for _, entry := range next.Entries[len(current.Entries):] {
		if entry.Kind != kind {
			return errors.New("conversation snapshot appended an unexpected entry kind")
		}
	}
	return nil
}

func validateConversationTrim(current, next *conversationSnapshot, removeIDs []string) error {
	if err := validateConversationSnapshot(current); err != nil {
		return err
	}
	if err := validateConversationSnapshot(next); err != nil {
		return err
	}
	if len(removeIDs) == 0 || next.Revision != current.Revision+1 || next.Sequence != current.Sequence {
		return errors.New("conversation trim snapshot is stale or invalid")
	}
	removed := make(map[string]struct{}, len(removeIDs))
	for _, id := range removeIDs {
		removed[id] = struct{}{}
	}
	kept := make([]conversationEntry, 0, len(current.Entries)-len(removed))
	for _, entry := range current.Entries {
		if _, remove := removed[entry.ID]; !remove {
			kept = append(kept, entry)
		}
	}
	if len(kept) != len(next.Entries) {
		return errors.New("conversation trim snapshot does not match the accepted plan")
	}
	for index := range kept {
		if !conversationEntryEqual(kept[index], next.Entries[index]) {
			return errors.New("conversation trim snapshot does not match the accepted plan")
		}
	}
	return nil
}

func conversationEntryEqual(left, right conversationEntry) bool {
	if left.ID != right.ID || left.Kind != right.Kind || len(left.Messages) != len(right.Messages) {
		return false
	}
	for index := range left.Messages {
		if !conversationJSONEqual(left.Messages[index], right.Messages[index]) {
			return false
		}
	}
	return true
}

func conversationSnapshotEqual(left, right *conversationSnapshot) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if left.Revision != right.Revision || left.Sequence != right.Sequence || len(left.Entries) != len(right.Entries) {
		return false
	}
	for index := range left.Entries {
		if !conversationEntryEqual(left.Entries[index], right.Entries[index]) {
			return false
		}
	}
	return true
}

func conversationJSONEqual(left, right json.RawMessage) bool {
	decode := func(raw json.RawMessage) (any, error) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	}
	leftValue, leftErr := decode(left)
	rightValue, rightErr := decode(right)
	return leftErr == nil && rightErr == nil && reflect.DeepEqual(leftValue, rightValue)
}
