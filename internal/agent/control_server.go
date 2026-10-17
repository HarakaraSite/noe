package agent

import (
	"crypto/subtle"
	"net/http"
	"sort"
	"strings"
)

// Human control shares the manager, but never the delegated child capability.
func (b *broker) serveControl(w http.ResponseWriter, r *http.Request) {
	if b.control == nil || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+b.control.connection.Token)) != 1 {
		writeRPCError(w, http.StatusUnauthorized, "authentication", "invalid control capability")
		return
	}
	h := b.host
	op := &Operation{host: h, ctx: h.ctx, control: true}
	switch strings.TrimPrefix(r.URL.Path, "/control/") {
	case "agents":
		var request struct{}
		if decodeRequest(r, &request) != nil {
			writeRPCError(w, 400, "request", "invalid control request")
			return
		}
		listing := ControlListing{HostID: b.control.connection.HostID, PID: b.control.connection.PID, Agents: []AgentSnapshot{}}
		h.mu.Lock()
		for _, instance := range h.instances {
			if instance.state != "stopped" && instance.state != "failed" {
				listing.Agents = append(listing.Agents, instance.snapshotLocked())
			}
		}
		h.mu.Unlock()
		sort.Slice(listing.Agents, func(i, j int) bool { return listing.Agents[i].AgentID < listing.Agents[j].AgentID })
		writeJSON(w, listing)
	case "steer":
		b.handleSteer(w, r, op)
	case "queue":
		b.handleQueue(w, r, op)
	case "interrupt":
		b.handleInterrupt(w, r, op)
	default:
		writeRPCError(w, http.StatusNotFound, "method", "unknown control request method")
	}
}
