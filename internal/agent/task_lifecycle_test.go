package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

// serve selects under Host.mu, then waits for command acceptance outside it.
// Pin that boundary directly so cancellation/stop wins before start without
// relying on scheduler timing or adding a production test hook.
func TestSelectedCommandCancelledOrStoppedBeforeStart(t *testing.T) {
	for _, stop := range []bool{false, true} {
		name, wantError, wantStart := "cancel", "agent task cancelled", commandCancelledBeforeStart
		if stop {
			name, wantError, wantStart = "stop", "interrupted", commandReturnedToQueue
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			cancelEntered, releaseCancel := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			h := NewHost(Options{Observe: func(event events.Event) {
				if event["type"] == "interrupt.accepted" {
					close(cancelEntered)
					<-releaseCancel
				}
			}})
			defer h.Close()
			defer releaseOnce.Do(func() { close(releaseCancel) })
			owner := h.NewOperation(ctx)
			instanceCtx, instanceCancel := context.WithCancel(owner.ctx)
			instance := &agentInstance{
				host: h, id: "agent", owner: owner, state: "starting",
				ctx: instanceCtx, cancel: instanceCancel, done: make(chan struct{}),
				commands: make(map[string]*command),
			}
			cmd := newTaskCommand("selected", "activity", "must not execute")
			h.mu.Lock()
			h.instances[instance.id] = instance
			owner.instances[instance.id] = instance
			instance.enqueueCommandLocked(cmd)
			selected := instance.selectCommandLocked()
			h.mu.Unlock()
			if selected != cmd {
				t.Fatal("selection did not reserve the queue head")
			}

			// Continue exactly the post-selection part of serve once acceptance
			// is released; serve itself then settles any returned reservation.
			applied := make(chan commandStartDisposition, 1)
			go func() {
				<-cmd.accepted
				h.mu.Lock()
				start := instance.startSelectedCommandLocked(cmd)
				h.mu.Unlock()
				applied <- start
				if start == commandCancelledBeforeStart {
					instance.publishCommandOutcome(cmd)
				}
				instance.serve()
			}()
			defer func() {
				select {
				case <-cmd.accepted:
				default:
					close(cmd.accepted)
				}
			}()

			// Publish acceptance before blocking the serialized observer on the
			// cancellation event. Close its channel only after cancellation
			// admission, pinning serve's event-to-start gate without reentering
			// Host.observe while the cancellation observer holds observeMu.
			instance.commandEvent(cmd, "accepted", "")

			var err error
			if stop {
				_, err = h.stopAgent(owner, StopAgentRequest{AgentID: instance.id})
			} else {
				_, err = h.cancelTask(owner, CancelTaskRequest{AgentID: instance.id, CommandID: cmd.id})
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-cancelEntered:
			case <-ctx.Done():
				t.Fatal("cancellation acceptance did not enter its observer")
			}
			select {
			case <-applied:
				t.Fatal("selected command advanced before acceptance was published")
			default:
			}
			close(cmd.accepted)
			select {
			case got := <-applied:
				if got != wantStart {
					t.Fatalf("selected-to-start result=%v, want %v", got, wantStart)
				}
			case <-ctx.Done():
				t.Fatal("selected command did not advance after acceptance")
			}
			select {
			case <-cmd.done:
				t.Fatal("task waiter released before cancellation acceptance was published")
			default:
			}
			releaseOnce.Do(func() { close(releaseCancel) })
			outcome, err := h.waitTask(owner, taskRequest{AgentID: instance.id, CommandID: cmd.id}, ctx)
			if err != nil || outcome.State != "cancelled" || outcome.Error != wantError || outcome.RunID != "" {
				t.Fatalf("selected command outcome=%+v err=%v", outcome, err)
			}
			select {
			case <-instance.done:
			case <-ctx.Done():
				t.Fatal("instance did not finish settling its selected command")
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			if instance.selected != nil || cmd.active || len(instance.queue) != 0 || instance.currentRunID != "" {
				t.Fatal("cancelled selection started a task or retained a scheduling reservation")
			}
		})
	}
}
