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
	response, err := b.host.spawn(op, req)
	if err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, response)
}

func (b *broker) handleWaitTask(w http.ResponseWriter, r *http.Request, op *Operation) {
	var req taskRequest
	if err := decodeRequest(r, &req); err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "agentId and commandId are required")
		return
	}
	outcome, err := b.host.waitTask(op, req, r.Context())
	if errors.Is(err, errRequestCancelled) {
		return
	}
	if err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, outcome)
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
