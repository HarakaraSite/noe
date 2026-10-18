package agent

import (
	"net/http"
)

func (b *broker) handleInterrupt(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request interruptRequest
	if err := decodeRequest(r, &request); err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid interrupt request")
		return
	}
	response, err := b.host.interrupt(op, request)
	if err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, response)
}
