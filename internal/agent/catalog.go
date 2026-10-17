package agent

import (
	"net/http"

	"noe/internal/agent/provider"
)

func (b *broker) handleListModels(w http.ResponseWriter, r *http.Request) {
	var query provider.ModelQuery
	if err := decodeRequest(r, &query); err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid model catalog query")
		return
	}
	models, err := b.host.catalog.ListModels(query)
	if err != nil {
		writeRPCError(w, http.StatusBadRequest, "configuration", err.Error())
		return
	}
	writeJSON(w, models)
}
