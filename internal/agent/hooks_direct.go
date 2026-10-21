package agent

import (
	"fmt"
	"strings"
)

type resolvedHook struct {
	Specifier string
	Canonical string
}

// RecordResolvedHook records the result of resolving one configured hook with
// the loader used by this task. Resolution is intentionally deferred until the
// execution runtime exists; the Host stores the original specifier and index.
func (op *Operation) RecordResolvedHook(index int, specifier, canonical string) error {
	if op == nil || op.host == nil {
		return ErrOperationClosed
	}
	if index < 0 || strings.TrimSpace(specifier) == "" || strings.TrimSpace(canonical) == "" {
		return fmt.Errorf("hook resolution requires a nonnegative index and nonempty specifier and canonical ID")
	}
	h := op.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if op.closed || op.ctx.Err() != nil || h.closed {
		return newOperationError(409, "scope", "hook resolution is outside an active direct execution")
	}
	var registered []string
	var run *agentRun
	if op.task != nil && op.task.instance != nil {
		run = op.task.run
		if run != nil && (run.ctx.Err() != nil || run.finalClosed || run.providerCalls != 0 || run.modelStep != nil) {
			return newOperationError(409, "scope", "hook resolution must finish before model execution starts")
		}
		registered = op.task.instance.config.hookModules()
	} else if owner := h.currentOwnerRunLocked(op); owner != nil {
		run = owner
		registered = append([]string(nil), owner.config.Hooks...)
	} else {
		return newOperationError(409, "scope", "hook resolution requires a task or run owner")
	}
	if index >= len(registered) || registered[index] != specifier {
		return newOperationError(409, "hook", "hook resolution does not match the registered specifier and index")
	}
	if op.resolvedHooks == nil {
		op.resolvedHooks = make(map[int]resolvedHook)
	}
	if previous, exists := op.resolvedHooks[index]; exists {
		if previous.Specifier != specifier || previous.Canonical != canonical {
			return newOperationError(409, "hook", "hook resolution is already fixed for this task")
		}
		return nil
	}
	op.resolvedHooks[index] = resolvedHook{Specifier: specifier, Canonical: canonical}
	if run != nil {
		if run.resolvedHooks == nil {
			run.resolvedHooks = make(map[int]resolvedHook)
		}
		run.resolvedHooks[index] = resolvedHook{Specifier: specifier, Canonical: canonical}
	}
	return nil
}
