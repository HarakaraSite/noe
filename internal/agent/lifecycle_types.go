package agent

type spawnRequest struct {
	Config      taskConfig `json:"config"`
	Instruction *string    `json:"instruction"`
	KeepAlive   bool       `json:"keepAlive"`
}

type spawnResponse struct {
	AgentID string `json:"agentId"`
}

type instanceRequest struct {
	AgentID string `json:"agentId"`
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
