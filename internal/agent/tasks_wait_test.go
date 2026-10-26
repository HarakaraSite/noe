package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

type groupedWaitFixture struct {
	host     *Host
	owner    *Operation
	scope    *Operation
	run      *agentRun
	tasks    []TaskReference
	commands []*command
}

func newGroupedWaitFixture(t *testing.T, observe func(events.Event)) *groupedWaitFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	h := NewHost(Options{Observe: observe})
	t.Cleanup(h.Close)
	owner := h.NewOperation(ctx)
	t.Cleanup(owner.Close)
	runCtx, cancelRun := context.WithCancel(owner.ctx)
	run := &agentRun{
		id: "parent", operation: owner, ctx: runCtx, cancel: cancelRun, started: time.Now(),
		childScopes: make(map[*Operation]struct{}), instances: make(map[string]*agentInstance),
		steerByID: make(map[string]*steerCommand), finalDone: make(chan struct{}),
	}
	scope := h.NewOperation(ctx)
	t.Cleanup(scope.Close)
	f := &groupedWaitFixture{host: h, owner: owner, scope: scope, run: run}
	h.mu.Lock()
	h.runs[run.id] = run
	scope.parentRunID, scope.ownerRun = run.id, run
	run.childScopes[scope] = struct{}{}
	for _, id := range []string{"first", "second"} {
		cmd := &command{id: "task-" + id, done: make(chan struct{})}
		done := make(chan struct{})
		close(done) // Only command completion participates in this unit fixture.
		h.instances[id] = &agentInstance{
			host: h, id: id, owner: owner, parent: run, ctx: ctx, done: done,
			commands: map[string]*command{cmd.id: cmd},
		}
		f.tasks = append(f.tasks, TaskReference{AgentID: id, CommandID: cmd.id})
		f.commands = append(f.commands, cmd)
	}
	h.mu.Unlock()
	return f
}

func (f *groupedWaitFixture) complete(index int, answer string) {
	f.host.mu.Lock()
	f.commands[index].outcome = &TaskOutcome{
		RunID: "child-" + f.tasks[index].AgentID, CommandID: f.tasks[index].CommandID, State: "completed", Answer: answer,
	}
	f.commands[index].publishDoneLocked()
	f.host.mu.Unlock()
}

func (f *groupedWaitFixture) steer(t *testing.T, id string) {
	t.Helper()
	receipt, err := f.host.steer(f.owner, SteerRequest{RunID: f.run.id, CommandID: id, Instruction: id})
	if err != nil || receipt.CommandID != id {
		t.Fatalf("steer receipt=%+v err=%v", receipt, err)
	}
}

func (f *groupedWaitFixture) apply(t *testing.T, ids ...string) {
	t.Helper()
	f.host.mu.Lock()
	delivered := f.host.deliverPendingSteersLocked(f.run)
	f.host.mu.Unlock()
	if len(delivered) != len(ids) {
		t.Fatalf("delivered=%+v, want %v", delivered, ids)
	}
	if _, err := f.host.applySteers(f.owner, applySteersRequest{RunID: f.run.id, CommandIDs: ids}); err != nil {
		t.Fatal(err)
	}
}

type groupedWaitReply struct {
	result WaitTasksResult
	err    error
}

// Done is read while Host.mu is held, before the wait drops it for select.
// Receiving scans and then taking Host.mu proves registration without sleeps.
type waitScanContext struct {
	context.Context
	scans chan struct{}
}

func (ctx *waitScanContext) Done() <-chan struct{} {
	ctx.scans <- struct{}{}
	return ctx.Context.Done()
}

func startGroupedWait(t *testing.T, f *groupedWaitFixture, request WaitTasksRequest) (<-chan groupedWaitReply, <-chan struct{}) {
	t.Helper()
	scanCtx := &waitScanContext{Context: f.scope.ctx, scans: make(chan struct{}, 16)}
	reply := make(chan groupedWaitReply, 1)
	go func() {
		result, err := f.host.waitTasks(f.scope, request, scanCtx)
		reply <- groupedWaitReply{result, err}
	}()
	select {
	case <-scanCtx.scans:
	case <-f.scope.ctx.Done():
		t.Fatal("wait did not register")
	}
	f.host.mu.Lock()
	//lint:ignore SA2001 This lock/unlock pair waits for waitTasks to release Host.mu after registration before the test proceeds.
	f.host.mu.Unlock()
	return reply, scanCtx.scans
}

func takeGroupedWait(t *testing.T, ctx context.Context, replies <-chan groupedWaitReply) WaitTasksResult {
	t.Helper()
	select {
	case reply := <-replies:
		if reply.err != nil {
			t.Fatal(reply.err)
		}
		return reply.result
	case <-ctx.Done():
		t.Fatal("grouped wait did not return")
		return WaitTasksResult{}
	}
}

func TestWaitTasksPendingSteerRewaitAndCompletionRetainResults(t *testing.T) {
	f := newGroupedWaitFixture(t, nil)
	f.complete(0, "結論🙂")
	f.steer(t, "one")
	f.steer(t, "two")
	f.steer(t, "one") // Accepted retry must neither duplicate nor consume.
	include := false
	query := WaitTasksRequest{Tasks: []TaskReference{f.tasks[1], f.tasks[0]}, IncludeAnswer: &include}
	for range 2 {
		got, err := f.scope.CallTypedContext(f.scope.ctx, "wait_tasks", query)
		if err != nil {
			t.Fatal(err)
		}
		result := got.(WaitTasksResult)
		if result.Reason != "steer" || result.RunID != f.run.id || !reflect.DeepEqual(result.Pending, []TaskReference{f.tasks[1]}) || len(result.Completed) != 1 || result.Completed[0].AgentID != f.tasks[0].AgentID {
			t.Fatalf("partial steer result=%+v", result)
		}
		outcome := result.Completed[0].Outcome
		if outcome.Answer != "" || outcome.AnswerBytes == nil || *outcome.AnswerBytes != len("結論🙂") {
			t.Fatalf("answer metadata=%+v", outcome)
		}
	}
	if f.scope.ctx.Err() != nil || f.run.ctx.Err() != nil || f.commands[1].outcome != nil {
		t.Fatal("steer return cancelled work or fabricated an outcome")
	}
	if f.run.steerByID["one"].status != "accepted" || len(f.run.steerOrder) != 2 {
		t.Fatal("wait consumed the steer or retry created a second command")
	}
	f.apply(t, "one", "two")

	replies, _ := startGroupedWait(t, f, query)
	f.host.mu.Lock()
	wake := f.run.steerWake
	f.host.mu.Unlock()
	f.steer(t, "one") // Applied retry must not leave a permanently ready wake.
	f.host.mu.Lock()
	unchanged := wake == f.run.steerWake
	f.host.mu.Unlock()
	if !unchanged {
		t.Fatal("applied retry re-notified waiters")
	}
	select {
	case reply := <-replies:
		t.Fatalf("applied steer released a new wait: %+v", reply)
	default:
	}
	f.steer(t, "three")
	if result := takeGroupedWait(t, f.scope.ctx, replies); result.Reason != "steer" || len(result.Pending) != 1 {
		t.Fatalf("new steer did not release rewait: %+v", result)
	}

	// Both conditions are ready before entry: steer must win and every
	// completed result must still be included in the original input order.
	f.complete(1, "second answer")
	result, err := f.host.waitTasks(f.scope, query, f.scope.ctx)
	if err != nil || result.Reason != "steer" || len(result.Pending) != 0 || len(result.Completed) != 2 || result.Completed[0].AgentID != f.tasks[1].AgentID || result.Completed[1].AgentID != f.tasks[0].AgentID {
		t.Fatalf("completion/steer race=%+v err=%v", result, err)
	}
	f.apply(t, "three")
	result, err = f.host.waitTasks(f.scope, query, f.scope.ctx)
	if err != nil || result.Reason != "completed" || len(result.Completed) != 2 || len(result.Pending) != 0 {
		t.Fatalf("all completed=%+v err=%v", result, err)
	}
	outcome, err := f.host.waitTask(f.scope, TaskRequest{AgentID: f.tasks[0].AgentID, CommandID: f.tasks[0].CommandID}, f.scope.ctx)
	if err != nil || outcome.Answer != "結論🙂" {
		t.Fatalf("legacy retrieval lost retained result=%+v err=%v", outcome, err)
	}
	part, err := f.host.readTaskResult(f.scope, TaskResultRequest{AgentID: f.tasks[0].AgentID, CommandID: f.tasks[0].CommandID})
	if err != nil || part.Text != outcome.Answer {
		t.Fatalf("read retained result=%+v err=%v", part, err)
	}
}

func TestWaitTasksAcceptancePublicationPrecedesWakeAndCompletedReturn(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	f := newGroupedWaitFixture(t, func(event events.Event) {
		if event["type"] == "command.accepted" && event["operation"] == "steer" {
			close(entered)
			<-release
		}
	})
	replies, scans := startGroupedWait(t, f, WaitTasksRequest{Tasks: f.tasks})
	steered := make(chan error, 1)
	go func() {
		_, err := f.host.steer(f.owner, SteerRequest{RunID: f.run.id, CommandID: "blocked-publication", Instruction: "inspect children"})
		steered <- err
	}()
	select {
	case <-entered:
	case <-f.scope.ctx.Done():
		t.Fatal("observer was not reached")
	}
	// Completion wakes the request while acceptance is still inside observer.
	f.complete(0, "first answer")
	f.complete(1, "second answer")
	select {
	case <-scans:
		f.host.mu.Lock()
		f.host.mu.Unlock()
	case reply := <-replies:
		t.Fatalf("wait returned ahead of acceptance publication: %+v", reply)
	case <-f.scope.ctx.Done():
		t.Fatal("completion did not wake the request")
	}
	select {
	case reply := <-replies:
		t.Fatalf("wait returned while acceptance observer was blocked: %+v", reply)
	default:
	}
	once.Do(func() { close(release) })
	result := takeGroupedWait(t, f.scope.ctx, replies)
	if result.Reason != "steer" || len(result.Completed) != 2 || len(result.Pending) != 0 {
		t.Fatalf("publication race result=%+v", result)
	}
	if err := <-steered; err != nil {
		t.Fatal(err)
	}
}

func TestWaitTasksValidatesWholeSetBeforeSteerAndPreservesScope(t *testing.T) {
	f := newGroupedWaitFixture(t, nil)
	f.steer(t, "pending")
	foreign := f.host.NewOperation(f.scope.ctx)
	defer foreign.Close()
	f.host.mu.Lock()
	f.host.instances["foreign"] = &agentInstance{owner: foreign, commands: map[string]*command{"task": {done: make(chan struct{})}}}
	f.host.mu.Unlock()
	defer func() {
		f.host.mu.Lock()
		delete(f.host.instances, "foreign")
		f.host.mu.Unlock()
	}()
	for _, tasks := range [][]TaskReference{
		nil,
		{f.tasks[0], f.tasks[0]},
		{{AgentID: " ", CommandID: "task"}},
		{f.tasks[0], {AgentID: "foreign", CommandID: "task"}},
		{f.tasks[0], {AgentID: "first", CommandID: "missing"}},
	} {
		if _, err := f.scope.CallTypedContext(f.scope.ctx, "wait_tasks", WaitTasksRequest{Tasks: tasks}); err == nil {
			t.Fatalf("invalid/unowned set returned a steer: %+v", tasks)
		}
	}
	f.host.mu.Lock()
	f.scope.parentRunID = "missing-owner"
	f.host.mu.Unlock()
	if _, err := f.host.waitTasks(f.scope, WaitTasksRequest{Tasks: f.tasks}, f.scope.ctx); err == nil {
		t.Fatal("orphan generated scope became a standalone completion wait")
	}
	f.host.mu.Lock()
	f.scope.parentRunID = f.run.id
	f.host.mu.Unlock()
}

func TestWaitTasksRequestCancellationOwnerEndAndLegacyWait(t *testing.T) {
	f := newGroupedWaitFixture(t, nil)
	f.steer(t, "pending")
	// Existing wait_task remains completion-only despite pending owner steer.
	requestCtx, cancelRequest := context.WithCancel(f.scope.ctx)
	legacy := make(chan error, 1)
	go func() {
		_, err := f.scope.CallContext(requestCtx, "wait_task", `{"agentId":"first","commandId":"task-first"}`)
		legacy <- err
	}()
	cancelRequest()
	if err := <-legacy; !errors.Is(err, context.Canceled) {
		t.Fatalf("legacy wait returned on steer rather than request cancellation: %v", err)
	}
	cancelled, cancel := context.WithCancel(f.scope.ctx)
	cancel()
	if _, err := f.scope.CallTypedContext(cancelled, "wait_tasks", WaitTasksRequest{Tasks: f.tasks}); !errors.Is(err, context.Canceled) {
		t.Fatalf("request cancellation=%v", err)
	}
	if f.commands[0].outcome != nil || f.run.ctx.Err() != nil || f.scope.ctx.Err() != nil {
		t.Fatal("request cancellation changed task or owner")
	}
	f.apply(t, "pending")
	replies, _ := startGroupedWait(t, f, WaitTasksRequest{Tasks: f.tasks})
	// The generated scope context is deliberately independent. Owner-run
	// cancellation itself must release the wait, not a context side effect.
	f.run.cancel()
	select {
	case reply := <-replies:
		if reply.err == nil || reply.result.Reason == "steer" {
			t.Fatalf("owner cancellation looked like a steer=%+v", reply)
		}
	case <-f.scope.ctx.Done():
		t.Fatal("owner end did not release grouped wait")
	}
}

func TestWaitTasksJSONAndTypedSnapshots(t *testing.T) {
	f := newGroupedWaitFixture(t, nil)
	f.complete(0, "answer")
	f.complete(1, "second")
	request := WaitTasksRequest{Tasks: append([]TaskReference(nil), f.tasks...), IncludeAnswer: new(bool)}
	snapshotValue, err := snapshotTypedRequest("wait_tasks", request)
	if err != nil {
		t.Fatal(err)
	}
	request.Tasks[0].AgentID = "changed"
	*request.IncludeAnswer = true
	snapshot := snapshotValue.(WaitTasksRequest)
	if snapshot.Tasks[0] != f.tasks[0] || *snapshot.IncludeAnswer {
		t.Fatal("typed input aliases caller slice/pointer")
	}
	invalid := "a\xff\xfe"
	normalized, err := snapshotTypedRequest("wait_tasks", WaitTasksRequest{Tasks: []TaskReference{{AgentID: invalid, CommandID: invalid}}})
	if err != nil || normalized.(WaitTasksRequest).Tasks[0].AgentID != "a��" || normalized.(WaitTasksRequest).Tasks[0].CommandID != "a��" {
		t.Fatalf("typed UTF-8 normalization=%+v err=%v", normalized, err)
	}
	usage := json.RawMessage(`{"total_tokens":4}`)
	bytes := 6
	retained := WaitTasksResult{Reason: "steer", RunID: invalid,
		Completed: []CompletedTask{{AgentID: invalid, Outcome: TaskOutcome{Answer: "secret", omitAnswer: true, AnswerBytes: &bytes, ErrorDetails: &imageapi.Error{Message: invalid, Usage: usage}}}},
		Pending:   []TaskReference{{AgentID: invalid, CommandID: invalid}},
	}
	copy := cloneWaitTasksResult(retained)
	copy.Completed[0].Outcome.ErrorDetails.Usage[0] = '!'
	copy.Pending[0].AgentID = "changed"
	*copy.Completed[0].Outcome.AnswerBytes = 1
	if retained.Completed[0].Outcome.ErrorDetails.Usage[0] != '{' || retained.Pending[0].AgentID != invalid || bytes != 6 || copy.RunID != "a��" || copy.Completed[0].Outcome.ErrorDetails.Message != "a��" || copy.Completed[0].Outcome.Answer != "" {
		t.Fatal("typed response aliases retained state or lost UTF-8/answer projection")
	}
	wire := controlCall(t, f.scope, "wait_tasks", snapshot)
	var result struct {
		Reason    string `json:"reason"`
		Completed []struct {
			AgentID string         `json:"agentId"`
			Outcome map[string]any `json:"outcome"`
		} `json:"completed"`
		Pending []TaskReference `json:"pending"`
	}
	if err := json.Unmarshal([]byte(wire), &result); err != nil || result.Reason != "completed" || result.Pending == nil || len(result.Completed) != 2 {
		t.Fatalf("wire result=%s err=%v", wire, err)
	}
	if _, exists := result.Completed[0].Outcome["answer"]; exists || result.Completed[0].Outcome["answerBytes"] != float64(len("answer")) {
		t.Fatalf("wire answer projection=%s", wire)
	}
}

func TestWaitTasksOwnerCloseJoinsAdmittedRequest(t *testing.T) {
	f := newGroupedWaitFixture(t, nil)
	entered, returned := make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := f.host.withDirectRequest(f.scope.ctx, f.scope, "", func(ctx context.Context, op *Operation) (any, error) {
			close(entered) // The actual direct request is in the close join now.
			return f.host.waitTasks(op, WaitTasksRequest{Tasks: f.tasks}, ctx)
		})
		returned <- err
	}()
	select {
	case <-entered:
	case <-f.scope.ctx.Done():
		t.Fatal("request was not admitted")
	}
	joinCtx, cancelJoin := context.WithTimeout(context.Background(), time.Second)
	defer cancelJoin()
	closed := make(chan struct{})
	go func() {
		f.owner.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-joinCtx.Done():
		t.Fatal("owner close hung joining grouped wait")
	}
	if err := <-returned; err == nil {
		t.Fatal("owner close did not reject its admitted wait")
	}
}

func TestWaitTasksAboveGoSelectCeilingWakesAndReleasesRegistrations(t *testing.T) {
	f := newGroupedWaitFixture(t, nil)
	// This is the Go reflection ceiling, not a product task-count setting.
	const count = 65536
	f.tasks = make([]TaskReference, count)
	f.commands = make([]*command, count)
	f.host.mu.Lock()
	instance := f.host.instances["first"]
	for index := range f.tasks {
		id := "overflow-" + strconv.Itoa(index)
		cmd := &command{id: id, done: make(chan struct{})}
		f.tasks[index] = TaskReference{AgentID: instance.id, CommandID: id}
		f.commands[index] = cmd
		instance.commands[id] = cmd
	}
	f.host.mu.Unlock()
	replies, scans := startGroupedWait(t, f, WaitTasksRequest{Tasks: f.tasks})
	f.host.mu.Lock()
	registered := len(f.commands[count-1].doneWaiters)
	f.host.mu.Unlock()
	if registered != 1 {
		t.Fatal("overflow command did not register its request-local wake")
	}
	f.complete(count-1, "overflow answer")
	select {
	case <-scans:
		f.host.mu.Lock()
		f.host.mu.Unlock()
	case reply := <-replies:
		t.Fatalf("large wait returned before all tasks or steer: %+v", reply)
	case <-f.scope.ctx.Done():
		t.Fatal("completion outside reflect.Select cases did not wake request")
	}
	f.steer(t, "large-wait-steer")
	result := takeGroupedWait(t, f.scope.ctx, replies)
	if result.Reason != "steer" || len(result.Completed) != 1 || result.Completed[0].Outcome.Answer != "overflow answer" || len(result.Pending) != count-1 {
		t.Fatalf("large wait lost completed result: reason=%s completed=%d pending=%d", result.Reason, len(result.Completed), len(result.Pending))
	}
	f.host.mu.Lock()
	defer f.host.mu.Unlock()
	for _, cmd := range f.commands {
		if len(cmd.doneWaiters) != 0 {
			t.Fatal("finished request left a completion registration")
		}
	}
}
