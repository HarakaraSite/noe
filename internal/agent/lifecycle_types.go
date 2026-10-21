package agent

type SpawnRequest struct {
	Config          TaskConfig `json:"config"`
	Instruction     *string    `json:"instruction"`
	KeepAlive       bool       `json:"keepAlive"`
	AllowDelegation *bool      `json:"allowDelegation,omitempty"`
}

type spawnRequest = SpawnRequest

type SpawnResponse struct {
	AgentID   string `json:"agentId"`
	CommandID string `json:"commandId,omitempty"`
}

type spawnResponse = SpawnResponse

type InstanceRequest struct {
	AgentID       string `json:"agentId"`
	IncludeAnswer *bool  `json:"includeAnswer,omitempty"`
}

type instanceRequest = InstanceRequest

type TaskRequest struct {
	AgentID       string `json:"agentId"`
	CommandID     string `json:"commandId"`
	IncludeAnswer *bool  `json:"includeAnswer,omitempty"`
}

type taskRequest = TaskRequest

type QueueRequest struct {
	AgentID     string `json:"agentId"`
	Instruction string `json:"instruction"`
	CommandID   string `json:"commandId,omitempty"`
}

type queueRequest = QueueRequest

type CommandReceipt struct {
	CommandID string `json:"commandId"`
	Status    string `json:"status"`
}

type commandReceipt = CommandReceipt

// Offsets and lengths refer to UTF-8 bytes in the retained original answer.
type TaskResultRequest struct {
	AgentID   string `json:"agentId"`
	CommandID string `json:"commandId"`
	Offset    int64  `json:"offset"`
	MaxBytes  *int64 `json:"maxBytes,omitempty"`
}

type taskResultRequest = TaskResultRequest

type TaskResult struct {
	RunID      string `json:"runId"`
	CommandID  string `json:"commandId"`
	State      string `json:"state"`
	Text       string `json:"text"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"nextOffset"`
	TotalBytes int64  `json:"totalBytes"`
	EOF        bool   `json:"eof"`
}

type taskResultSlice = TaskResult

type CancelTaskRequest struct {
	AgentID   string `json:"agentId"`
	CommandID string `json:"commandId"`
}

type CancelTaskResponse struct {
	AgentID   string `json:"agentId"`
	CommandID string `json:"commandId"`
	Status    string `json:"status"`
}

type StopAgentRequest struct {
	AgentID string `json:"agentId"`
}

type StopAgentResponse struct {
	AgentID string `json:"agentId"`
	Status  string `json:"status"`
}
