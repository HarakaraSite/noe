package agent

type spawnRequest struct {
	Config          taskConfig `json:"config"`
	Instruction     *string    `json:"instruction"`
	KeepAlive       bool       `json:"keepAlive"`
	AllowDelegation *bool      `json:"allowDelegation,omitempty"`
}

type spawnResponse struct {
	AgentID   string `json:"agentId"`
	CommandID string `json:"commandId,omitempty"`
}

type instanceRequest struct {
	AgentID       string `json:"agentId"`
	IncludeAnswer *bool  `json:"includeAnswer,omitempty"`
}

type taskRequest struct {
	AgentID       string `json:"agentId"`
	CommandID     string `json:"commandId"`
	IncludeAnswer *bool  `json:"includeAnswer,omitempty"`
}

type queueRequest struct {
	AgentID     string `json:"agentId"`
	Instruction string `json:"instruction"`
	CommandID   string `json:"commandId,omitempty"`
}

type commandReceipt struct {
	CommandID string `json:"commandId"`
	Status    string `json:"status"`
}

// Offsets and lengths refer to UTF-8 bytes in the retained original answer.
type taskResultRequest struct {
	AgentID   string `json:"agentId"`
	CommandID string `json:"commandId"`
	Offset    int64  `json:"offset"`
	MaxBytes  *int64 `json:"maxBytes,omitempty"`
}

type taskResultSlice struct {
	RunID      string `json:"runId"`
	CommandID  string `json:"commandId"`
	State      string `json:"state"`
	Text       string `json:"text"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"nextOffset"`
	TotalBytes int64  `json:"totalBytes"`
	EOF        bool   `json:"eof"`
}
