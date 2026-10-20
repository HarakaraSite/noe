package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestDelegationAdmissionRequiresOwnersGrant(t *testing.T) {
	for _, tc := range []struct {
		name    string
		grant   []bool
		allowed bool
	}{
		{name: "child-default"},
		{name: "child-denied", grant: []bool{false}},
		{name: "child-granted", grant: []bool{true}, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHost(Options{Provider: &fixtureProvider{}})
			defer h.Close()
			owner := h.NewOperation(context.Background())
			defer owner.Close()
			rootID := spawnLifecycle(t, owner, nil, true)
			h.mu.Lock()
			root := h.instances[rootID]
			h.mu.Unlock()
			rootOp, rootRun := newManagedTestRun(t, h, root, "root-run", "root-activity", "root-command", "")
			defer rootOp.Close()
			childID := spawnLifecycle(t, rootOp, nil, true, tc.grant...)
			h.mu.Lock()
			child := h.instances[childID]
			h.mu.Unlock()
			childOp, childRun := newManagedTestRun(t, h, child, "child-run", "child-activity", "child-command", rootRun.id)
			defer childOp.Close()
			h.mu.Lock()
			before := root.group.counters.AgentStarts
			h.mu.Unlock()
			// Setting permission on the requested instance cannot grant the
			// calling child permission to create it. Exercise the broker directly.
			selfGrant := true
			request, err := json.Marshal(spawnRequest{
				Config:    taskConfig{Config: Config{Provider: "openrouter", Model: "fixture/model", Name: "grandchild", Effort: "auto"}},
				KeepAlive: true, AllowDelegation: &selfGrant,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = childOp.Call("spawn_agent", string(request))
			if tc.allowed {
				if err != nil {
					t.Fatalf("explicitly authorized delegation: %v", err)
				}
			} else {
				var rpcErr *RPCError
				if !errors.As(err, &rpcErr) || rpcErr.Code != "delegation" {
					t.Fatalf("unauthorized delegation returned %v", err)
				}
				h.mu.Lock()
				unchanged := root.group.counters.AgentStarts == before && root.group.stop == nil && len(childRun.instances) == 0
				h.mu.Unlock()
				if !unchanged {
					t.Fatal("rejected launch started an instance or stopped the group")
				}
				// The parent remains able to do useful work after the rejection.
				spawnLifecycle(t, rootOp, nil, true)
			}
		})
	}
}
