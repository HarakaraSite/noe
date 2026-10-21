package agent

import (
	"sort"
)

// listAgents is available only on trusted Go control operations. It returns
// snapshots rather than exposing mutable instance state to callers.
func (h *Host) listAgents(op *Operation) ([]InstanceSnapshot, error) {
	if op == nil || op.host != h || !op.control {
		return nil, newOperationError(403, "scope", "host management is only available to a trusted Go operation")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if op.closed || op.ctx.Err() != nil || h.closed {
		return nil, newOperationError(409, "scope", "agent operation has ended")
	}
	ids := make([]string, 0, len(h.instances))
	for id := range h.instances {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	snapshots := make([]InstanceSnapshot, 0, len(ids))
	for _, id := range ids {
		if instance := h.instances[id]; instance != nil {
			snapshots = append(snapshots, instance.snapshotLocked())
		}
	}
	return snapshots, nil
}
