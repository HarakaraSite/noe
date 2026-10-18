package agent

// Access follows ownership, not the short-lived process which sent spawn.
// An owning evaluation can also manage its nested descendants.
func (h *Host) canManageLocked(op *Operation, instance *agentInstance) bool {
	if op.control {
		return true
	}
	currentRun := h.currentOwnerRunLocked(op)
	// A generated scope belongs to the run that launched it. Let that run
	// manage its own instance without granting sibling scopes access.
	if instance.currentRunID != "" && (op.parentRunID == instance.currentRunID || currentRun != nil && currentRun.id == instance.currentRunID) {
		return true
	}
	if instance.parent == nil {
		return instance.owner == op
	}
	for parent := instance.parent; parent != nil; {
		if parent.operation == op || parent.id == op.parentRunID || currentRun != nil && parent.id == currentRun.id {
			return true
		}
		if parent.instance == nil {
			break
		}
		if parent.instance.parent == nil && parent.instance.owner == op {
			return true
		}
		parent = parent.instance.parent
	}
	return false
}

// canManageRunLocked applies the same owner and parent-scope relationship used
// for managed instances to an active task run. The run's own operation and a
// generated scope created by that run may manage it directly.
func (h *Host) canManageRunLocked(op *Operation, run *agentRun) bool {
	if op == nil || run == nil {
		return false
	}
	if op.control {
		return true
	}
	currentRun := h.currentOwnerRunLocked(op)
	if run.operation == op || op.parentRunID == run.id || currentRun != nil && currentRun.id == run.id {
		return true
	}
	if run.instance != nil && h.canManageLocked(op, run.instance) {
		return true
	}
	for parentID := run.parentRunID; parentID != ""; {
		parent := h.runs[parentID]
		if parent == nil {
			break
		}
		if parent.operation == op || op.parentRunID == parent.id || currentRun != nil && parent.id == currentRun.id {
			return true
		}
		if parent.instance != nil && h.canManageLocked(op, parent.instance) {
			return true
		}
		parentID = parent.parentRunID
	}
	return false
}

// OwnedLifetime lets a local runtime track a spawned instance as native work.
// Remote run_js clients only track their launch request; the parent host owns
// the instance and its process lifetime.
func (op *Operation) OwnedLifetime(id string) <-chan struct{} {
	h := op.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if instance := op.instances[id]; instance != nil {
		return instance.done
	}
	return nil
}
