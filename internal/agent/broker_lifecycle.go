package agent

import (
	"errors"
	"net/http"
)

func (b *broker) handleSpawn(w http.ResponseWriter, r *http.Request, op *Operation) {
	var req spawnRequest
	if err := decodeRequest(r, &req); err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid agent configuration")
		return
	}
	id, err := b.host.spawn(op, req)
	if err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, spawnResponse{AgentID: id})
}

func (b *broker) handleInstance(w http.ResponseWriter, r *http.Request, op *Operation, wait bool) {
	var req instanceRequest
	if err := decodeRequest(r, &req); err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "agentId is required")
		return
	}
	snapshot, err := b.host.observeInstance(op, req, wait, r.Context())
	if errors.Is(err, errRequestCancelled) {
		return
	}
	if err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, snapshot)
}

func (b *broker) handleQueue(w http.ResponseWriter, r *http.Request, op *Operation) {
	var req queueRequest
	if err := decodeRequest(r, &req); err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "agentId and instruction are required")
		return
	}
	response, err := b.host.queue(op, req)
	if err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, response)
}
