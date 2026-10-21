package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agentbridge"
	"forge.harakara.site/littleisland/noe/v2/internal/agentexec"
	"forge.harakara.site/littleisland/noe/v2/internal/jsexec"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type InstructionSource func(context.Context) (string, error)

// HostConfig borrows services and callbacks. Runtime is a template for fresh
// task/tool executions, with stdio reserved for the executor's own capture.
type HostConfig struct {
	Models       model.Service
	Runtime      *stdlib.Profile
	Instructions InstructionSource
	Policy       string
	Observe      func(Event)
	Trace        io.Writer
}

type Host struct {
	inner *core.Host
	admin *Client
}

func NewHost(ctx context.Context, cfg HostConfig) (*Host, error) {
	if ctx == nil {
		return nil, errors.New("agent: context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Models == nil {
		return nil, errors.New("agent: Models is required")
	}
	var executor *agentexec.Executor
	if cfg.Runtime != nil {
		env := cfg.Runtime.Environment
		if env.Stdin != nil || env.Stdout != nil || env.Stderr != nil {
			return nil, errors.New("agent: Runtime stdio must be nil; task and tool streams are owned by the executor")
		}
		profile, err := jsexec.Snapshot(*cfg.Runtime)
		if err != nil {
			return nil, err
		}
		executor = &agentexec.Executor{Profile: profile}
	}
	options := core.Options{Models: cfg.Models, Context: ctx, Observe: cfg.Observe, Trace: cfg.Trace, Instructions: cfg.Instructions, Policy: cfg.Policy}
	if executor != nil {
		options.Execute = executor.Execute
	}
	host := core.NewHost(options)
	if executor != nil {
		executor.Host = host
	}
	admin := host.NewControlOperation(ctx)
	return &Host{inner: host, admin: &Client{op: admin}}, nil
}

func (h *Host) Close() {
	if h != nil && h.inner != nil {
		h.inner.Close()
	}
}

// NewClient creates one ownership scope; closing it cancels and joins its
// requests and agents. Waiting with a shorter request context is observational.
func (h *Host) NewClient(ctx context.Context) (*Client, error) {
	if h == nil || h.inner == nil || ctx == nil {
		return nil, errors.New("agent: live Host and context are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	op := h.inner.NewOperation(ctx)
	if err := op.Context().Err(); err != nil {
		op.Close()
		return nil, err
	}
	return &Client{op: op}, nil
}

type Client struct{ op *core.Operation }

func (c *Client) Context() context.Context { return c.op.Context() }
func (c *Client) Close() {
	if c != nil && c.op != nil {
		c.op.CloseClient()
	}
}
func (c *Client) Models() model.Service { return c.op.Models() }

type HookModules = core.HookModules
type ContextBudget = core.ContextBudget
type ContextPolicy = core.ContextPolicy
type ExecutionLimits = core.ExecutionLimits
type AgentConfig = core.Config
type TaskConfig = core.TaskConfig
type SpawnRequest = core.SpawnRequest
type SpawnResponse = core.SpawnResponse
type InstanceRequest = core.InstanceRequest
type InstanceSnapshot = core.InstanceSnapshot
type QueueRequest = core.QueueRequest
type CommandReceipt = core.CommandReceipt
type TaskRequest = core.TaskRequest
type TaskOutcome = core.TaskOutcome
type TaskResultRequest = core.TaskResultRequest
type TaskResult = core.TaskResult
type SteerRequest = core.SteerRequest
type CancelTaskRequest = core.CancelTaskRequest
type CancelTaskResponse = core.CancelTaskResponse
type StopAgentRequest = core.StopAgentRequest
type StopAgentResponse = core.StopAgentResponse

func clientCall[T any](ctx context.Context, c *Client, method string, request any) (result T, err error) {
	if ctx == nil || c == nil || c.op == nil {
		return result, errors.New("agent: live Client and context are required")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	response, err := c.op.CallContext(ctx, method, string(payload))
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(response), &result)
	return result, err
}
func (c *Client) Spawn(ctx context.Context, q SpawnRequest) (SpawnResponse, error) {
	return clientCall[SpawnResponse](ctx, c, "spawn_agent", q)
}
func (c *Client) Run(ctx context.Context, q SpawnRequest) (InstanceSnapshot, error) {
	accepted, err := c.Spawn(ctx, q)
	if err != nil {
		return InstanceSnapshot{}, err
	}
	return c.Wait(ctx, InstanceRequest{AgentID: accepted.AgentID})
}
func (c *Client) Queue(ctx context.Context, q QueueRequest) (CommandReceipt, error) {
	return clientCall[CommandReceipt](ctx, c, "queue_agent", q)
}
func (c *Client) Observe(ctx context.Context, q InstanceRequest) (InstanceSnapshot, error) {
	return clientCall[InstanceSnapshot](ctx, c, "observe_agent", q)
}
func (c *Client) Wait(ctx context.Context, q InstanceRequest) (InstanceSnapshot, error) {
	return clientCall[InstanceSnapshot](ctx, c, "wait_agent", q)
}
func (c *Client) WaitTask(ctx context.Context, q TaskRequest) (TaskOutcome, error) {
	return clientCall[TaskOutcome](ctx, c, "wait_task", q)
}
func (c *Client) ReadTaskResult(ctx context.Context, q TaskResultRequest) (TaskResult, error) {
	return clientCall[TaskResult](ctx, c, "read_task_result", q)
}
func (c *Client) Steer(ctx context.Context, q SteerRequest) (CommandReceipt, error) {
	return clientCall[CommandReceipt](ctx, c, "steer_agent", q)
}
func (c *Client) CancelTask(ctx context.Context, q CancelTaskRequest) (CancelTaskResponse, error) {
	return clientCall[CancelTaskResponse](ctx, c, "cancel_task", q)
}
func (c *Client) StopAgent(ctx context.Context, q StopAgentRequest) (StopAgentResponse, error) {
	return clientCall[StopAgentResponse](ctx, c, "stop_agent", q)
}

// Attach borrows the Host and execution view. Its binding owns only a new Client.
func Attach(ctx context.Context, view *stdlib.Bindings, host *Host) (*Binding, error) {
	if view == nil || view.Context() == nil || view.Worker() {
		return nil, errors.New("agent: live non-Worker bindings are required")
	}
	client, err := host.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	binding, err := agentbridge.AttachClient(client.Context(), view, host.inner, client.op)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &Binding{inner: binding}, nil
}

// ListAgents and the following methods are trusted Go management operations.
// JavaScript bindings receive only their own Client's ownership scope.
func (h *Host) ListAgents(ctx context.Context) ([]InstanceSnapshot, error) {
	return clientCall[[]InstanceSnapshot](ctx, h.admin, "list_agents", struct{}{})
}
func (h *Host) Queue(ctx context.Context, q QueueRequest) (CommandReceipt, error) {
	return h.admin.Queue(ctx, q)
}
func (h *Host) Steer(ctx context.Context, q SteerRequest) (CommandReceipt, error) {
	return h.admin.Steer(ctx, q)
}
func (h *Host) CancelTask(ctx context.Context, q CancelTaskRequest) (CancelTaskResponse, error) {
	return h.admin.CancelTask(ctx, q)
}
func (h *Host) StopAgent(ctx context.Context, q StopAgentRequest) (StopAgentResponse, error) {
	return h.admin.StopAgent(ctx, q)
}
