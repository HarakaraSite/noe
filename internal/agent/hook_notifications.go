package agent

import (
	"encoding/json"
	"strings"
)

// A preparation binds the exact ephemeral status message to the invocation IDs
// it reports. It is run-local, never part of a conversation continuation.
type preparedContextNotification struct {
	text           string
	hookFailureIDs []string
}

func pendingHookFailures(run *agentRun) []HookFailure {
	var pending []HookFailure
	for _, failure := range run.hookFailures {
		if failure.NotifiedRequestID == "" {
			pending = append(pending, failure)
		}
	}
	return pending
}

func hookFailureNotice(failures []HookFailure) string {
	if len(failures) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\nHook processing failures for this task:\n")
	out.WriteString("The following hook handlers failed. Task processing continued with the last valid values; side effects completed before a failure may remain.\n")
	for _, failure := range failures {
		// JSON keeps arbitrary error text separate from the factual metadata.
		data, _ := json.Marshal(failure)
		out.WriteString("- ")
		out.Write(data)
		out.WriteByte('\n')
	}
	return out.String()
}

func (n *preparedContextNotification) matches(messages []json.RawMessage) bool {
	if len(messages) == 0 {
		return false
	}
	var tail struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	return json.Unmarshal(messages[len(messages)-1], &tail) == nil &&
		tail.Role == "user" && tail.Content == n.text
}

func markHookFailuresNotified(run *agentRun, notice *preparedContextNotification, requestID string) {
	if notice == nil || len(notice.hookFailureIDs) == 0 {
		return
	}
	included := make(map[string]bool, len(notice.hookFailureIDs))
	for _, id := range notice.hookFailureIDs {
		included[id] = true
	}
	for index := range run.hookFailures {
		failure := &run.hookFailures[index]
		if failure.NotifiedRequestID == "" && included[failure.InvocationID] {
			failure.NotifiedRequestID = requestID
		}
	}
}
