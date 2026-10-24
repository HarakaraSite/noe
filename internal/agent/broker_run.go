package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

type agentRun struct {
	id                      string
	operation               *Operation
	ctx                     context.Context
	cancel                  context.CancelFunc
	started                 time.Time
	config                  runConfig
	parentRunID             string
	group                   *executionGroup
	activityID              string
	causedBy                []string
	origin                  activityOrigin
	childScopes             map[*Operation]struct{}
	instances               map[string]*agentInstance
	instance                *agentInstance
	steerByID               map[string]*steerCommand
	steerOrder              []*steerCommand
	pendingSteerActivityIDs []string
	finalAnswer             string
	finalDone               chan struct{}
	finalClosed             bool
	finalEventDone          bool
	providerCalls           int
	interrupting            bool
	modelStep               *modelStepState
	context                 contextRunState
	resolvedHooks           map[int]resolvedHook
	images                  map[string]*imageapi.Data
	refusal                 *imageapi.Error
}

// modelStepState retains only the current logical judgment and the IDs needed
// to connect its provider response and tool calls to hook activity.
type modelStepState struct {
	id                      string
	activityID              string
	predecessorActivityIDs  []string
	beforeHookActivityIDs   []string
	requestCauseActivityIDs []string
	requestID               string
	requestActivityID       string
	responseComplete        bool
	finished                bool
	toolCalls               map[string]*toolCallState
	toolCallOrder           []string
	imagesClosed            bool
	imagesCommitted         bool
}

type toolCallState struct {
	name           string
	outputBytes    int64
	activityID     string
	started        bool
	resultRecorded bool
	imageOrders    map[int]bool
	rawImages      []*imageapi.Data
	images         []*imageapi.Data
	imagesReady    bool
}

func (r *agentRun) agentID() string {
	if r.instance != nil {
		return r.instance.id
	}
	return ""
}

// A managed instance keeps one provider session across all of its queued runs.
// Unmanaged internal runs have no instance and retain their own run boundary.
func (r *agentRun) sessionID() string {
	if r.instance != nil {
		return r.instance.id
	}
	return r.id
}

type runConfig struct {
	Provider             string
	Model                string
	Name                 string
	MaxTokens            int
	Effort               string
	MaxSteps             int
	Hooks                []string
	ContextBudget        *ContextBudget
	ContextBudgetPresent bool
	ContextPolicy        *ContextPolicy
	ContextPolicyPresent bool
}

type startRequest struct {
	Provider      string          `json:"provider"`
	Model         string          `json:"model"`
	Name          string          `json:"name"`
	MaxTokens     int             `json:"maxTokens"`
	Effort        string          `json:"effort,omitempty"`
	MaxSteps      int             `json:"maxSteps,omitempty"`
	Hooks         []string        `json:"hooks,omitempty"`
	ContextBudget json.RawMessage `json:"contextBudget,omitempty"`
	ContextPolicy json.RawMessage `json:"contextPolicy,omitempty"`
}

type finishRequest struct {
	RunID  string `json:"runId"`
	Status string `json:"status"`
	Answer string `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (h *Host) start(requestContext context.Context, op *Operation, request startRequest) (any, error) {
	requestedBudget, budgetPresent, budgetErr := parseContextBudget(request.ContextBudget)
	requestedPolicy, policyPresent, policyErr := parseContextPolicy(request.ContextPolicy)
	effort, validEffort := provider.NormalizeEffort(request.Effort)
	if budgetErr != nil || policyErr != nil || strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.Name) == "" || request.MaxTokens < 0 || request.MaxSteps < 0 || !validEffort {
		return nil, newOperationError(http.StatusBadRequest, "request", "invalid start request")
	}
	requestCtx, cancelRequest := requestOperationContext(requestContext, op)
	defer cancelRequest()
	hookModules := append([]string(nil), request.Hooks...)
	id, err := randomID()
	if err != nil {
		return nil, newOperationError(http.StatusInternalServerError, "broker", "could not allocate agent run")
	}
	activityID, err := randomID()
	if err != nil {
		return nil, newOperationError(http.StatusInternalServerError, "broker", "could not allocate agent activity")
	}
	var task *managedTask
	var managedInstance *agentInstance
	var managedCommand bool
	var managedBase, managedConversation *conversationSnapshot
	var taskInstruction, explicitSystem string
	var userInstructions string
	var instructionErr error
	h.mu.Lock()
	task = op.task
	if task != nil && task.instance != nil {
		managedInstance = task.instance
		managedBase = cloneConversationSnapshot(task.instance.conversation)
		explicitSystem = task.instance.config.System
		managedCommand = task.command != nil
		if managedCommand {
			taskInstruction = task.command.instruction
		}
	}
	h.mu.Unlock()
	if !managedCommand {
		userInstructions, instructionErr = h.loadInstructions(requestCtx)
	} else if managedBase != nil {
		managedConversation = cloneConversationSnapshot(managedBase)
		if err := managedConversation.appendProtected(taskInstruction); err != nil {
			instructionErr = err
		}
	} else {
		userInstructions, instructionErr = h.loadInstanceInstructions(requestCtx, managedInstance)
		if instructionErr == nil {
			managedConversation, instructionErr = initialConversation(composeSystemMessage(h.instructionFacts(), userInstructions, explicitSystem), taskInstruction)
		}
	}
	if instructionErr == nil {
		if err := h.checkProvider(requestCtx, request.Provider); err != nil {
			return nil, newOperationError(http.StatusBadRequest, "provider", err.Error())
		}
	}
	ctx, cancel := context.WithCancel(op.ctx)
	config := runConfig{
		Provider: request.Provider, Model: request.Model,
		Name: request.Name, MaxTokens: request.MaxTokens, Effort: effort, MaxSteps: request.MaxSteps,
		Hooks: hookModules, ContextBudget: cloneContextBudget(requestedBudget), ContextBudgetPresent: budgetPresent,
		ContextPolicy: cloneContextPolicy(requestedPolicy), ContextPolicyPresent: policyPresent,
	}
	run := &agentRun{
		id:        id,
		operation: op, ctx: ctx, cancel: cancel, started: time.Now(),
		config: config, parentRunID: op.parentRunID,
		activityID:  activityID,
		childScopes: make(map[*Operation]struct{}),
		instances:   make(map[string]*agentInstance),
		steerByID:   make(map[string]*steerCommand),
		finalDone:   make(chan struct{}),
	}
	var resolvedModelWindow, resolvedMaxOutput *int64
	if instructionErr == nil {
		resolvedModelWindow, resolvedMaxOutput = h.contextModelLimitsContext(requestCtx, request.Provider, request.Model)
	}
	if requestCtx.Err() != nil {
		cancel()
		return nil, newOperationError(http.StatusConflict, "cancelled", requestCtx.Err().Error())
	}
	h.mu.Lock()
	if op.closed || op.ctx.Err() != nil || h.closed {
		h.mu.Unlock()
		cancel()
		return nil, newOperationError(http.StatusConflict, "scope", "agent operation has ended")
	}
	if op.task != nil {
		instance := op.task.instance
		if instance == nil {
			h.mu.Unlock()
			cancel()
			return nil, newOperationError(http.StatusConflict, "scope", "agent task has no instance")
		}
		if !h.executionGroupOpenLocked(instance.group) {
			group := instance.group
			h.mu.Unlock()
			cancel()
			code, message := executionLimitError(group)
			return nil, newOperationError(http.StatusConflict, code, message)
		}
		if instance.state == "stopping" || instance.state == "stopped" || instance.state == "failed" || instance.ctx.Err() != nil {
			h.mu.Unlock()
			cancel()
			return nil, newOperationError(http.StatusConflict, "scope", "agent instance is stopping or finished")
		}
		run.group = instance.group
		run.config.Hooks = instance.config.hookModules()
		run.config.ContextBudget = cloneContextBudget(instance.config.ContextBudget)
		run.config.ContextBudgetPresent = instance.config.contextBudgetPresent
		run.config.ContextPolicy = cloneContextPolicy(instance.config.ContextPolicy)
		run.config.ContextPolicyPresent = instance.config.contextPolicyPresent || instance.config.ContextPolicy != nil
		run.context.modelWindow = cloneInt64(instance.modelWindow)
		run.context.catalogMaxOutput = cloneInt64(instance.modelMaxOutputTokens)
		run.origin = activityOrigin{Kind: "script", AgentID: instance.id}
		if op.task.command != nil {
			run.origin.CommandID = op.task.command.id
		}
		if op.task.command != nil && op.task.command.activityID != "" {
			run.causedBy = []string{op.task.command.activityID}
		}
	} else {
		owner := h.currentOwnerRunLocked(op)
		run.group = h.executionForOperationLocked(op)
		run.context.modelWindow = cloneInt64(resolvedModelWindow)
		run.context.catalogMaxOutput = cloneInt64(resolvedMaxOutput)
		run.origin = activityOrigin{Kind: "script"}
		if owner != nil {
			run.origin.RunID = owner.id
			if owner.activityID != "" {
				run.causedBy = []string{owner.activityID}
			}
		}
	}
	run.context.budget = cloneContextBudget(run.config.ContextBudget)
	run.context.policy = cloneContextPolicy(run.config.ContextPolicy)
	run.context.policyDisabled = run.config.ContextPolicyPresent && run.config.ContextPolicy == nil
	run.context.maxTokens = run.config.MaxTokens
	h.runs[id] = run
	if op.task != nil {
		if op.task.run != nil {
			delete(h.runs, id)
			h.mu.Unlock()
			cancel()
			return nil, newOperationError(http.StatusConflict, "run", "task scope already claimed a run")
		}
		if managedCommand && !conversationSnapshotEqual(op.task.instance.conversation, managedBase) {
			delete(h.runs, id)
			h.mu.Unlock()
			cancel()
			return nil, newOperationError(http.StatusConflict, "conversation", "agent conversation changed while starting the task")
		}
		if managedCommand && instructionErr == nil && managedConversation != nil {
			op.task.instance.conversation = cloneConversationSnapshot(managedConversation)
		}
		if len(op.resolvedHooks) > 0 {
			run.resolvedHooks = make(map[int]resolvedHook, len(op.resolvedHooks))
			for index, resolved := range op.resolvedHooks {
				run.resolvedHooks[index] = resolved
			}
		}
		run.context.restoreConversation(op.task.instance.conversationContext)
		op.task.bindRunLocked(run)
		h.storeContextSnapshotLocked(run, time.Now())
	}
	h.mu.Unlock()
	h.traceEvent(traceEvent{
		RunID: id, ParentID: run.parentRunID, Name: config.Name, Model: config.Model,
		Status: "started",
	})
	h.observeRun("agent.started", id, run, events.Event{"status": "started"})
	startResponse := map[string]any{"runId": id, "sessionId": run.sessionID()}
	if instructionErr != nil {
		startResponse["instructionFailed"] = true
		startResponse["instructionError"] = instructionErr.Error()
	}
	if managedCommand {
		if instructionErr == nil && managedConversation != nil {
			startResponse["snapshot"] = cloneConversationSnapshot(managedConversation)
		}
	} else {
		startResponse["instructions"] = userInstructions
	}
	return startResponse, nil
}

func (h *Host) finish(ctx context.Context, op *Operation, request finishRequest) (any, error) {
	if ctx == nil {
		return nil, newOperationError(http.StatusBadRequest, "request", "finish context is required")
	}
	if request.RunID == "" {
		return nil, newOperationError(http.StatusBadRequest, "request", "invalid finish request")
	}
	status := sanitizeRunStatus(request.Status)
	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || run.operation != op {
		h.mu.Unlock()
		return nil, newOperationError(http.StatusConflict, "run", "unknown agent run")
	}
	if run.finalClosed && status != "completed" {
		h.mu.Unlock()
		return nil, newOperationError(http.StatusConflict, "final", "a committed final answer cannot be replaced by a failed finish")
	}
	var emitFinal bool
	var finalSettled []<-chan struct{}
	var waitFinal <-chan struct{}
	if status == "completed" {
		if run.finalClosed {
			if strings.TrimSpace(request.Answer) == "" || request.Answer != run.finalAnswer {
				h.mu.Unlock()
				return nil, newOperationError(http.StatusConflict, "final", "finish answer does not match the committed final answer")
			}
			waitFinal = run.finalDone
		} else if strings.TrimSpace(request.Answer) != "" {
			// Preserve direct-run finish behavior for existing internal callers.
			// Managed task execution must pass the final-answer gate first.
			if run.instance != nil {
				h.mu.Unlock()
				return nil, newOperationError(http.StatusConflict, "final", "managed task must commit its final answer first")
			}
			for _, command := range run.steerOrder {
				if command.status == "accepted" || command.status == "delivered" {
					h.mu.Unlock()
					return nil, newOperationError(http.StatusConflict, "final", "pending steer commands must be applied before finishing")
				}
			}
			finalSettled = run.commitFinalAnswerLocked(request.Answer)
			emitFinal = true
		} else if run.instance != nil {
			h.mu.Unlock()
			return nil, newOperationError(http.StatusConflict, "final", "managed task completion requires a committed answer")
		}
	}
	pending := h.cancelPendingSteersLocked(run)
	discardStepImages(run.modelStep)
	run.images = nil
	delete(h.runs, request.RunID)
	if op.task != nil && op.task.run == run {
		report := request
		op.task.report = &report
	}
	run.cancel()
	childScopes := make([]*Operation, 0, len(run.childScopes))
	for childScope := range run.childScopes {
		childScopes = append(childScopes, childScope)
	}
	clear(run.childScopes)
	instances := make([]*agentInstance, 0, len(run.instances))
	for _, instance := range run.instances {
		instances = append(instances, instance)
	}
	h.mu.Unlock()
	h.emitSteerEvents(pending)
	if emitFinal {
		h.publishCommittedFinal(run, request.Answer, finalSettled)
	} else if waitFinal != nil {
		select {
		case <-waitFinal:
		case <-op.ctx.Done():
			return nil, op.ctx.Err()
		case <-ctx.Done():
			return nil, errRequestCancelled
		}
	}
	for _, childScope := range childScopes {
		childScope.Close()
	}
	for _, instance := range instances {
		<-instance.done
	}
	if run.instance != nil {
		return map[string]bool{}, nil
	}
	h.traceEvent(traceEvent{
		RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
		Status: status, Duration: time.Since(run.started),
	})
	fields := events.Event{
		"status": status, "duration": time.Since(run.started),
	}
	if status == "failed" && request.Error != "" {
		fields["error"] = request.Error
	}
	if run.refusal != nil {
		fields["errorDetails"] = run.refusal
	}
	h.observeRun("agent."+status, request.RunID, run, fields)
	return map[string]bool{}, nil
}

func sanitizeRunStatus(status string) string {
	switch status {
	case "completed", "failed", "cancelled":
		return status
	default:
		return "failed"
	}
}
