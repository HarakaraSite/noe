package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

var lifecycleBuild struct {
	sync.Once
	dir, binary string
	err         error
	output      []byte
}

func TestMain(m *testing.M) {
	code := m.Run()
	if lifecycleBuild.dir != "" {
		if err := os.RemoveAll(lifecycleBuild.dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

func lifecycleBinary(t *testing.T) string {
	t.Helper()
	lifecycleBuild.Do(func() {
		lifecycleBuild.dir, lifecycleBuild.err = os.MkdirTemp("", "noe-agent-tests-")
		if lifecycleBuild.err != nil {
			return
		}
		lifecycleBuild.binary = filepath.Join(lifecycleBuild.dir, "noe")
		if runtime.GOOS == "windows" {
			lifecycleBuild.binary += ".exe"
		}
		_, file, _, _ := runtime.Caller(0)
		cmd := exec.Command("go", "build", "-o", lifecycleBuild.binary, "./cmd/noe")
		cmd.Dir = filepath.Dir(filepath.Dir(filepath.Dir(file)))
		lifecycleBuild.output, lifecycleBuild.err = cmd.CombinedOutput()
	})
	if lifecycleBuild.err != nil {
		t.Fatalf("build managed-task executable: %v\n%s", lifecycleBuild.err, lifecycleBuild.output)
	}
	return lifecycleBuild.binary
}

func lifecycleCall(t *testing.T, op *Operation, method string, payload any) string {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := op.Call(method, string(encoded))
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return result
}
func spawnLifecycle(t *testing.T, op *Operation, instruction *string, keepAlive bool, allowDelegation ...bool) string {
	t.Helper()
	req := spawnRequest{Config: taskConfig{Config: Config{Provider: "openrouter", Model: "fixture/model", Name: "managed", Effort: "auto"}, MaxSteps: 1}, Instruction: instruction, KeepAlive: keepAlive}
	if len(allowDelegation) > 0 {
		req.AllowDelegation = &allowDelegation[0]
	}
	result := lifecycleCall(t, op, "spawn_agent", req)
	var spawned instanceRequest
	if err := json.Unmarshal([]byte(result), &spawned); err != nil || spawned.AgentID == "" {
		t.Fatalf("spawn result %s: %v", result, err)
	}
	return spawned.AgentID
}
func lifecycleSnapshot(t *testing.T, op *Operation, id string, wait bool) instanceSnapshot {
	t.Helper()
	method := "observe_agent"
	if wait {
		method = "wait_agent"
	}
	text := lifecycleCall(t, op, method, instanceRequest{AgentID: id})
	var snapshot instanceSnapshot
	if err := json.Unmarshal([]byte(text), &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func awaitLifecycleEvent(t *testing.T, ch <-chan events.Event, kind string) events.Event {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-ch:
			if event["type"] == kind {
				return event
			}
		case <-timer.C:
			t.Fatalf("waiting for %s", kind)
			return nil
		}
	}
}

func TestLifecycleIdleFIFOAndFailureRecovery(t *testing.T) {
	binary := lifecycleBinary(t)
	var mu sync.Mutex
	var requests []provider.Request
	p := &fixtureProvider{gen: func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()
		var user struct {
			Content string `json:"content"`
		}
		for index := len(req.Messages) - 1; index >= 0; index-- {
			var m struct{ Role, Content string }
			if json.Unmarshal(req.Messages[index], &m) == nil && m.Role == "user" && !strings.HasPrefix(m.Content, "NOE_CONTEXT_STATUS\n") {
				user.Content = m.Content
				break
			}
		}
		if user.Content == "fail" {
			return provider.Response{}, errors.New("fixture failure")
		}
		body, _ := json.Marshal(map[string]string{"role": "assistant", "content": user.Content})
		return provider.Response{Message: body, FinishReason: "stop"}, nil
	}}
	observed := make(chan events.Event, 100)
	h := NewHost(Options{Provider: p, Executable: binary, Cwd: t.TempDir(), Observe: func(event events.Event) { observed <- event }})
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	op := h.NewOperation(ctx)
	defer op.Close()
	id := spawnLifecycle(t, op, nil, true)
	awaitLifecycleEvent(t, observed, "instance.idle")
	if p.callCount() != 0 {
		t.Fatal("idle instance called the model")
	}
	for _, text := range []string{"first", "fail", "third"} {
		lifecycleCall(t, op, "queue_agent", queueRequest{AgentID: id, Instruction: text, CommandID: text})
	}
	for index, want := range []string{"completed", "failed", "completed"} {
		event := awaitLifecycleEvent(t, observed, "command."+want)
		if event["commandId"] != []string{"first", "fail", "third"}[index] {
			t.Fatalf("FIFO event: %#v", event)
		}
	}
	awaitLifecycleEvent(t, observed, "instance.idle")
	snapshot := lifecycleSnapshot(t, op, id, false)
	if snapshot.State != "idle" || snapshot.QueueLength != 0 || snapshot.LastResult == nil || snapshot.LastResult.Answer != "third" {
		t.Fatalf("snapshot: %+v", snapshot)
	}
	// A replay returns its previous outcome; a conflicting replay is rejected.
	replay := lifecycleCall(t, op, "queue_agent", queueRequest{AgentID: id, Instruction: "third", CommandID: "third"})
	if !strings.Contains(replay, `"status":"completed"`) {
		t.Fatalf("replay: %s", replay)
	}
	if _, err := op.Call("queue_agent", fmt.Sprintf(`{"agentId":%q,"instruction":"different","commandId":"third"}`, id)); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("model calls=%d", len(requests))
	}
	seen := map[string]bool{}
	for index, req := range requests {
		messages := transcriptWithoutContextStatus(t, req.Messages)
		if len(messages) != []int{2, 4, 5}[index] || req.CacheKey != id || req.SessionID != id || req.RunID == id || req.RunID == "" || seen[req.RunID] {
			t.Fatalf("task continuation/cache boundary incorrect: %+v", req)
		}
		seen[req.RunID] = true
	}
}

func TestLifecycleExplicitCacheKeyAcrossQueue(t *testing.T) {
	binary := lifecycleBinary(t)
	for _, key := range []string{"explicit-cache", ""} {
		t.Run(fmt.Sprintf("key=%q", key), func(t *testing.T) {
			requests := make(chan provider.Request, 2)
			p := &fixtureProvider{gen: func(_ context.Context, req provider.Request) (provider.Response, error) {
				requests <- req
				return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"done"}`), FinishReason: "stop"}, nil
			}}
			h := NewHost(Options{Provider: p, Executable: binary, Cwd: t.TempDir()})
			defer h.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			op := h.NewOperation(ctx)
			defer op.Close()
			var spawned spawnResponse
			result := lifecycleCall(t, op, "spawn_agent", spawnRequest{
				Config:    taskConfig{Config: Config{Provider: "openrouter", Model: "fixture/model", Name: "managed", Effort: "auto"}, MaxSteps: 1, CacheKey: &key},
				KeepAlive: true,
			})
			if err := json.Unmarshal([]byte(result), &spawned); err != nil {
				t.Fatal(err)
			}
			var previousRun string
			for _, instruction := range []string{"first", "second"} {
				var receipt commandReceipt
				queued := lifecycleCall(t, op, "queue_agent", queueRequest{AgentID: spawned.AgentID, Instruction: instruction})
				if err := json.Unmarshal([]byte(queued), &receipt); err != nil {
					t.Fatal(err)
				}
				lifecycleCall(t, op, "wait_task", taskRequest{AgentID: spawned.AgentID, CommandID: receipt.CommandID})
				req := <-requests
				if req.SessionID != spawned.AgentID || req.CacheKey != key || req.RunID == "" || req.RunID == previousRun || req.RunID == req.SessionID {
					t.Fatalf("queued session/cache settings: %+v", req)
				}
				previousRun = req.RunID
			}
		})
	}
}

func TestLifecycleSpawnReturnsBeforeModelAndScopesAreIsolated(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	p := &fixtureProvider{gen: func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		close(started)
		select {
		case <-release:
			return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"done"}`), FinishReason: "stop"}, nil
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		}
	}}
	h := NewHost(Options{Provider: p, Executable: lifecycleBinary(t), Cwd: t.TempDir()})
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	op := h.NewOperation(ctx)
	defer op.Close()
	other := h.NewOperation(ctx)
	defer other.Close()
	text := "task"
	id := spawnLifecycle(t, op, &text, false)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("task did not start")
	}
	for _, method := range []string{"observe_agent", "wait_agent", "queue_agent"} {
		if _, err := other.Call(method, fmt.Sprintf(`{"agentId":%q,"instruction":"foreign"}`, id)); err == nil {
			t.Fatalf("foreign scope %s accepted", method)
		}
	}
	if _, err := op.Call("queue_agent", fmt.Sprintf(`{"agentId":%q,"instruction":"extra"}`, id)); err == nil {
		t.Fatal("single-task agent accepted queue")
	}
	once.Do(func() { close(release) })
	snapshot := lifecycleSnapshot(t, op, id, true)
	if snapshot.State != "stopped" || snapshot.Reason != "task_completed" || snapshot.LastResult == nil || snapshot.LastResult.Answer != "done" {
		t.Fatalf("terminal: %+v", snapshot)
	}
}

func TestLifecycleChildOutlivesGeneratedScopeButNotParentRun(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	p := &fixtureProvider{gen: func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return provider.Response{}, ctx.Err()
	}}
	h := NewHost(Options{Provider: p, Executable: lifecycleBinary(t), Cwd: t.TempDir()})
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	parentOp := h.NewOperation(ctx)
	defer parentOp.Close()
	parentID := startFixtureRun(t, parentOp)
	h.mu.Lock()
	parent := h.runs[parentID]
	h.mu.Unlock()
	generated := h.NewOperation(parent.ctx)
	h.mu.Lock()
	generated.parentRunID = parentID
	parent.childScopes[generated] = struct{}{}
	h.mu.Unlock()
	text := "child task"
	id := spawnLifecycle(t, generated, &text, true)
	generated.Close()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("child did not survive generated scope")
	}
	snapshot := lifecycleSnapshot(t, parentOp, id, false)
	if snapshot.State != "running" || snapshot.ParentRunID != parentID {
		t.Fatalf("child not parent-owned: %+v", snapshot)
	}
	if err := finishFixtureRun(parentOp, parentID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("parent finish returned before child provider stopped")
	}
	snapshot = lifecycleSnapshot(t, parentOp, id, false)
	if snapshot.State != "stopped" || snapshot.Reason != "owner_cancelled" || snapshot.LastResult == nil || snapshot.LastResult.State != "cancelled" {
		t.Fatalf("child survived parent: %+v", snapshot)
	}
}

func TestLifecycleFatalFailureClosesQueueAdmissionBeforeCancellingReservations(t *testing.T) {
	cancelling, releaseCancel := make(chan struct{}), make(chan struct{})
	var releaseOnce, sawCancel sync.Once
	h := NewHost(Options{Provider: &fixtureProvider{}, Executable: filepath.Join(t.TempDir(), "missing-noe"), Observe: func(event events.Event) {
		if event["type"] == "command.cancelled" {
			sawCancel.Do(func() { close(cancelling); <-releaseCancel })
		}
	}})
	defer func() { releaseOnce.Do(func() { close(releaseCancel) }); h.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op := h.NewOperation(ctx)
	id := spawnLifecycle(t, op, nil, true)
	// Seed two already accepted reservations under the manager lock so launch
	// failure cannot happen before both reservations exist.
	h.mu.Lock()
	instance := h.instances[id]
	for _, text := range []string{"initial", "reserved"} {
		accepted := make(chan struct{})
		close(accepted)
		cmd := &command{id: text, instruction: text, status: "accepted", accepted: accepted, done: make(chan struct{})}
		instance.queue = append(instance.queue, cmd)
		instance.commands[text] = cmd
	}
	h.mu.Unlock()
	instance.wake <- struct{}{}
	select {
	case <-cancelling:
	case <-ctx.Done():
		t.Fatal("fatal failure did not cancel reservation")
	}
	if _, err := op.Call("queue_agent", fmt.Sprintf(`{"agentId":%q,"instruction":"too late"}`, id)); err == nil {
		t.Error("fatal shutdown accepted a command after final queue drain")
	}
	releaseOnce.Do(func() { close(releaseCancel) })
	snapshot := lifecycleSnapshot(t, op, id, true)
	if snapshot.State != "failed" || snapshot.Reason != "supervision_failed" || snapshot.QueueLength != 0 {
		t.Fatalf("fatal snapshot: %+v", snapshot)
	}
	if got := taskWaitResult(t, op, id, "initial"); got.State != "failed" {
		t.Fatalf("fatal task wait result: %+v", got)
	}
	if got := taskWaitResult(t, op, id, "reserved"); got.State != "cancelled" || got.RunID != "" {
		t.Fatalf("fatal shutdown reservation wait result: %+v", got)
	}
	op.Close()
}

func TestLifecycleCancellationWaitsForCommandAcceptancePublication(t *testing.T) {
	stopping, cancelled := make(chan struct{}), make(chan struct{})
	idle := make(chan struct{})
	var mu sync.Mutex
	var order []string
	h := NewHost(Options{Provider: &fixtureProvider{}, Observe: func(event events.Event) {
		if event["type"] == "instance.idle" {
			close(idle)
		}
		if event["type"] == "instance.stopping" {
			close(stopping)
		}
		if event["commandId"] == "pending" {
			mu.Lock()
			order = append(order, event["type"].(string))
			mu.Unlock()
			if event["type"] == "command.cancelled" {
				close(cancelled)
			}
		}
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	op := h.NewOperation(ctx)
	id := spawnLifecycle(t, op, nil, true)
	select {
	case <-idle:
	case <-time.After(time.Second):
		t.Fatal("instance did not reach idle")
	}
	published := make(chan struct{})
	var publishOnce sync.Once
	defer func() { publishOnce.Do(func() { close(published) }); h.Close() }()
	// This is the interval in queue_agent between insertion and publication.
	// No wake has been sent yet, so cancellation must drain this reservation.
	cmd := &command{id: "pending", instruction: "pending", status: "accepted", accepted: published, done: make(chan struct{})}
	h.mu.Lock()
	instance := h.instances[id]
	instance.queue = append(instance.queue, cmd)
	instance.commands[cmd.id] = cmd
	h.mu.Unlock()
	cancel()
	select {
	case <-stopping:
	case <-time.After(time.Second):
		t.Fatal("owner cancellation did not start")
	}
	select {
	case <-cancelled:
		t.Fatal("cancelled event overtook acceptance")
	default:
	}
	instance.commandEvent(cmd, "accepted", "")
	publishOnce.Do(func() { close(published) })
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not finish after acceptance")
	}
	<-instance.done
	op.Close()
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(order, ",") != "command.accepted,command.cancelled" {
		t.Fatalf("command event order=%v", order)
	}
}
