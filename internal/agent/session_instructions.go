package agent

import (
	"context"
	jsonv1 "encoding/json"
	"net/http"
	"slices"
	"strings"
)

type SetInstructionRequest struct {
	AgentID     string `json:"agentId"`
	Name        string `json:"name"`
	Instruction string `json:"instruction"`
}

type RemoveInstructionRequest struct {
	AgentID string `json:"agentId"`
	Name    string `json:"name"`
}

type sessionInstruction struct {
	name string
	text string
}

func (h *Host) setInstruction(ctx context.Context, op *Operation, req SetInstructionRequest) (bool, error) {
	if strings.TrimSpace(req.Instruction) == "" {
		return false, newOperationError(http.StatusBadRequest, "request", "instruction must be nonempty")
	}
	return h.updateInstruction(ctx, op, req.AgentID, req.Name, req.Instruction)
}

func (h *Host) removeInstruction(ctx context.Context, op *Operation, req RemoveInstructionRequest) (bool, error) {
	return h.updateInstruction(ctx, op, req.AgentID, req.Name, "")
}

func (h *Host) updateInstruction(ctx context.Context, op *Operation, agentID, name, text string) (bool, error) {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(name) == "" {
		return false, newOperationError(http.StatusBadRequest, "request", "agentId and name are required")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	instance := h.instances[agentID]
	// The spawning caller can edit the system prompt it configured. A task's
	// generated scope can steer its own run but cannot edit that system prompt.
	if instance == nil || op == nil || (!op.control && instance.owner != op) {
		return false, newOperationError(http.StatusConflict, "agent", "unknown or unowned agent")
	}
	if h.closed || op.closed || op.ctx.Err() != nil || instance.ctx.Err() != nil ||
		(instance.state != "starting" && instance.state != "running" && instance.state != "idle") {
		return false, newOperationError(http.StatusConflict, "state", "agent does not accept instruction updates")
	}
	index := slices.IndexFunc(instance.sessionInstructions, func(instruction sessionInstruction) bool {
		return instruction.name == name
	})
	switch {
	case text == "" && index < 0:
		return false, nil
	case text == "":
		instance.sessionInstructions = slices.Delete(instance.sessionInstructions, index, index+1)
	case index < 0:
		instance.sessionInstructions = append(instance.sessionInstructions, sessionInstruction{name: name, text: text})
	case instance.sessionInstructions[index].text == text:
		return false, nil
	default:
		instance.sessionInstructions[index].text = text
	}
	return true, nil
}

// systemMessageWithInstructions builds the model step's private system
// message from the original protected entry and the current instruction set.
func systemMessageWithInstructions(conversation *conversationSnapshot, instructions []sessionInstruction) (jsonv1.RawMessage, error) {
	if len(instructions) == 0 {
		return nil, nil
	}
	var message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := jsonv1.Unmarshal(conversation.Entries[0].Messages[0], &message); err != nil {
		return nil, err
	}
	var content strings.Builder
	content.WriteString(message.Content)
	for _, instruction := range instructions {
		content.WriteString("\n\n")
		content.WriteString(instruction.text)
	}
	message.Content = content.String()
	return jsonv1.Marshal(message)
}
