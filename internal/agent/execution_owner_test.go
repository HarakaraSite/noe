package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestExecutionTaskScopeManagesOwnInstance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := NewHost(Options{})
	defer h.Close()
	owner := h.NewOperation(ctx)
	defer owner.Close()
	selfID := spawnLifecycle(t, owner, nil, true)
	siblingID := spawnLifecycle(t, owner, nil, true)
	h.mu.Lock()
	self := h.instances[selfID]
	h.mu.Unlock()
	bootstrap := h.NewOperation(self.ctx)
	defer bootstrap.Close()
	h.mu.Lock()
	bootstrap.task = &managedTask{instance: self, command: &command{id: "self-task"}}
	h.mu.Unlock()
	startFixtureRun(t, bootstrap)
	text := lifecycleCall(t, bootstrap, "observe_agent", instanceRequest{AgentID: selfID})
	var snapshot instanceSnapshot
	if err := json.Unmarshal([]byte(text), &snapshot); err != nil || snapshot.AgentID != selfID {
		t.Fatalf("task cannot observe its own instance: %s (%v)", text, err)
	}
	query, err := json.Marshal(instanceRequest{AgentID: siblingID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Call("observe_agent", string(query)); err == nil {
		t.Fatal("current-task binding granted access to an independent sibling")
	}
}

// A task bootstrap retains its initiating parentRunID. Ordinary imports must
// nevertheless launch work under that task's own run, just like generated JS.
func TestExecutionTaskScopeOwnsSpawnedAgent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := NewHost(Options{})
	defer h.Close()
	owner := h.NewOperation(ctx)
	defer owner.Close()
	ancestorID := startFixtureRun(t, owner)
	generated := h.NewOperation(ctx)
	defer generated.Close()
	h.mu.Lock()
	generated.parentRunID = ancestorID
	h.runs[ancestorID].childScopes[generated] = struct{}{}
	h.mu.Unlock()
	childID := spawnLifecycle(t, generated, nil, true)
	h.mu.Lock()
	child := h.instances[childID]
	h.mu.Unlock()
	bootstrap := h.NewOperation(child.ctx)
	defer bootstrap.Close()
	h.mu.Lock()
	bootstrap.parentRunID = ancestorID
	bootstrap.task = &managedTask{instance: child, command: &command{id: "bootstrap-command"}}
	h.mu.Unlock()
	currentID := startFixtureRun(t, bootstrap)
	grandchildID := spawnLifecycle(t, bootstrap, nil, true)
	h.mu.Lock()
	grandchild := h.instances[grandchildID]
	current := bootstrap.task.run
	correctParent := grandchild.parent == current && grandchild.parent.id == currentID
	sameGroup := grandchild.group == child.group && grandchild.group.id == childID
	unchangedBootstrapParent := bootstrap.parentRunID == ancestorID
	h.mu.Unlock()
	if !correctParent || !sameGroup || !unchangedBootstrapParent {
		t.Fatalf("ownership mismatch: currentParent=%v sameGroup=%v bootstrapParentPreserved=%v", correctParent, sameGroup, unchangedBootstrapParent)
	}
	bootstrap.Close()
	select {
	case <-grandchild.done:
	case <-ctx.Done():
		t.Fatal("closing current task left its spawned agent running")
	}
	h.mu.Lock()
	ancestorAlive := h.runs[ancestorID] != nil && h.runs[ancestorID].ctx.Err() == nil
	childAlive := child.ctx.Err() == nil
	h.mu.Unlock()
	if !ancestorAlive || !childAlive {
		t.Fatalf("closing task cancelled its ancestors: ancestor=%v instance=%v", ancestorAlive, childAlive)
	}
}

func TestExecutionMissingParentScopeCannotSpawnNewRoot(t *testing.T) {
	h := NewHost(Options{})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	h.mu.Lock()
	op.parentRunID = "finished-owner-run"
	h.mu.Unlock()
	_, err := op.Call("spawn_agent", `{"config":{"provider":"openrouter","model":"fixture/model","name":"orphan","effort":"auto"},"keepAlive":true}`)
	if err == nil {
		t.Fatal("a scope whose parent run is gone started an independent root")
	}
	h.mu.Lock()
	instances, groups := len(h.instances), len(h.groups)
	h.mu.Unlock()
	if instances != 0 || groups != 0 {
		t.Fatalf("rejected spawn left state: instances=%d groups=%d", instances, groups)
	}
}
