package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"noe/internal/events"
	"noe/internal/mdcli/markdown"
)

type agentRenderer struct {
	out   io.Writer
	width int
	open  bool
	key   string
}

func newAgentRenderer(out io.Writer) *agentRenderer {
	return &agentRenderer{out: out, width: observationWidth(out)}
}

func observationWidth(out io.Writer) int {
	if columns, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && columns > 0 {
		return columns
	}
	if file, ok := out.(*os.File); ok {
		if width, _, err := term.GetSize(int(file.Fd())); err == nil && width > 0 {
			return width
		}
	}
	return markdown.DefaultWidth
}

func eventString(event events.Event, key string) string {
	value, _ := event[key].(string)
	return value
}
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func eventLabel(event events.Event) string {
	name := eventString(event, "name")
	id := eventString(event, "runId")
	if id == "" {
		id = eventString(event, "agentId")
	}
	if id == "" {
		name = "script"
		id = eventString(event, "executionId")
	}
	if name == "" {
		name = "agent"
	}
	label := name + "/" + shortID(id)
	if toolID := eventString(event, "toolCallId"); toolID != "" {
		toolID = strings.TrimPrefix(strings.TrimPrefix(toolID, "call_"), "call-")
		label += "/" + shortID(toolID)
	}
	return label
}

func (r *agentRenderer) render(event events.Event) error {
	kind := eventString(event, "type")
	label := eventLabel(event)
	key := eventString(event, "executionId") + "/" + eventString(event, "runId") + "/" + eventString(event, "requestId") + "/" + eventString(event, "toolCallId") + "/" + kind
	text := eventString(event, "text")
	switch kind {
	case "context.updated":
		return r.line(label, "context", contextDetail(event["context"]))
	case "hook.started", "hook.failed", "hook.cancelled":
		detail := eventString(event, "phase") + " / " + eventString(event, "module")
		if text := eventString(event, "error"); text != "" {
			detail += " / " + text
		}
		return r.line(label, "hook "+strings.TrimPrefix(kind, "hook."), detail)
	case "execution.limit_reached":
		detail := eventString(event, "reason")
		if dimension := eventString(event, "limitName"); dimension != "" {
			detail = fmt.Sprintf("%s / used=%v / limit=%v", dimension, event["used"], event["limit"])
		}
		return r.line(label, "execution limit reached", detail)
	case "decision.started", "decision.completed", "decision.failed", "decision.cancelled":
		detail := eventString(event, "decisionModel") + " [" + shortID(eventString(event, "requestId")) + "]"
		if responseModel := eventString(event, "responseModel"); responseModel != "" && responseModel != eventString(event, "decisionModel") {
			detail += " -> " + responseModel
		}
		if duration, ok := event["duration"].(float64); ok {
			detail += " / " + time.Duration(duration).Round(time.Millisecond).String()
		}
		if reason := eventString(event, "reason"); reason != "" {
			detail += " / " + reason
		}
		return r.line(label, "decision "+strings.TrimPrefix(kind, "decision."), detail)
	case "instance.started", "instance.running", "instance.idle", "instance.stopping", "instance.stopped", "instance.failed":
		return r.line(label, "agent "+strings.TrimPrefix(kind, "instance."), eventString(event, "reason"))
	case "command.accepted", "command.applied", "command.cancelled":
		if eventString(event, "operation") == "steer" {
			return r.line(label, "steer "+eventString(event, "status"), eventString(event, "commandId"))
		}
		return nil
	case "interrupt.accepted":
		return r.line(label, "interrupt accepted", eventString(event, "target"))
	case "stdout":
		// Keep command output in the recording and tool result, not the human view.
		return nil
	case "thinking", "thinking_summary", "stderr":
		title := map[string]string{"thinking": "thinking", "thinking_summary": "thinking summary", "stderr": "stderr"}[kind]
		return r.fragment(key, label, title, text)
	case "message_delta":
		// The accepted full intermediate/final text is emitted separately.
		return nil
	case "agent.message":
		return r.block(label, "message", text)
	case "agent.final":
		return r.block(label, "final answer", text)
	case "run_js.started":
		if text == "" {
			text = "started"
		}
		return r.line(label, "run_js started", strings.TrimSpace(strings.TrimPrefix(text, "//")))
	case "run_js.finished":
		kind := "run_js failed"
		if event["success"] == true {
			kind = "run_js succeeded"
		}
		var parts []string
		if code, ok := event["exitCode"].(float64); ok && code != 0 {
			parts = append(parts, fmt.Sprintf("exitCode=%g", code))
		}
		if duration, ok := event["duration"].(float64); ok {
			parts = append(parts, time.Duration(duration).Round(time.Millisecond).String())
		}
		if reason := eventString(event, "reason"); reason != "" && reason != "completed" && reason != "failed" {
			parts = append(parts, reason)
		}
		return r.line(label, kind, strings.Join(parts, " / "))
	case "model.started":
		return r.line(label, "model", "waiting for response")
	case "model.failed":
		return r.line(label, "model failed", eventString(event, "reason"))
	case "agent.started":
		return r.line(label, "started", eventString(event, "model"))
	case "agent.completed", "agent.failed", "agent.cancelled":
		return r.line(label, map[string]string{"agent.completed": "completed", "agent.failed": "failed", "agent.cancelled": "cancelled"}[kind], eventString(event, "error"))
	case "execution.error", "repl.error", "repl.result":
		return r.line(label, map[string]string{"execution.error": "execution error", "repl.error": "evaluation error", "repl.result": "result"}[kind], text)
	case "execution.finished":
		return r.line(label, "finished", fmt.Sprintf("exitCode=%v", event["exitCode"]))
	}
	return nil
}

// The renderer accepts both live typed snapshots and decoded JSONL objects.
func contextDetail(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "unknown"
	}
	var snapshot struct {
		Budget struct {
			InputTokens *int64 `json:"inputTokens"`
		} `json:"budget"`
		ModelWindow struct {
			Tokens *int64 `json:"tokens"`
		} `json:"modelWindow"`
		Previous *struct {
			RequestID    string `json:"requestId"`
			Status       string `json:"status"`
			PromptTokens *int64 `json:"promptTokens"`
			CachedTokens *int64 `json:"cachedTokens"`
		} `json:"previous"`
		Estimate *struct {
			Tokens int64  `json:"tokens"`
			Stage  string `json:"stage"`
		} `json:"estimate"`
		RemainingTokens *int64 `json:"remainingTokens"`
	}
	if json.Unmarshal(encoded, &snapshot) != nil || string(encoded) == "null" {
		return "unknown"
	}
	number := func(n *int64) string {
		if n == nil {
			return "unknown"
		}
		return strconv.FormatInt(*n, 10)
	}
	parts := []string{"budget=" + number(snapshot.Budget.InputTokens), "window=" + number(snapshot.ModelWindow.Tokens)}
	if snapshot.Estimate != nil {
		parts = append(parts, fmt.Sprintf("estimated=%d (%s)", snapshot.Estimate.Tokens, snapshot.Estimate.Stage))
	} else {
		parts = append(parts, "estimated=unknown")
	}
	parts = append(parts, "remaining="+number(snapshot.RemainingTokens))
	if p := snapshot.Previous; p != nil {
		measurement := "previous=" + number(p.PromptTokens)
		if p.PromptTokens != nil {
			measurement += " measured"
		}
		parts = append(parts, measurement+" ["+shortID(p.RequestID)+"]", "cached="+number(p.CachedTokens), p.Status)
	} else {
		parts = append(parts, "previous=unknown")
	}
	return strings.Join(parts, " / ")
}

func (r *agentRenderer) fragment(key, label, kind, text string) error {
	if text == "" {
		return nil
	}
	grouped := kind == "thinking" || kind == "thinking summary"
	if r.key != "" && r.key != key {
		if err := r.finish(); err != nil {
			return err
		}
	}
	for _, part := range strings.SplitAfter(text, "\n") {
		if part == "" {
			continue
		}
		if !r.open {
			if !grouped || r.key != key {
				if _, err := fmt.Fprintf(r.out, "[%s] %s: ", label, kind); err != nil {
					return err
				}
			} else if part != "\n" {
				if _, err := io.WriteString(r.out, "  "); err != nil {
					return err
				}
			}
			r.open = true
			r.key = key
		}
		if _, err := io.WriteString(r.out, part); err != nil {
			return err
		}
		if strings.HasSuffix(part, "\n") {
			r.open = false
			if !grouped {
				r.key = ""
			}
		}
	}
	return nil
}

func (r *agentRenderer) line(label, kind, text string) error {
	if err := r.finish(); err != nil {
		return err
	}
	if text == "" {
		_, err := fmt.Fprintf(r.out, "[%s] %s\n", label, kind)
		return err
	}
	if err := r.fragment("line", label, kind, text); err != nil {
		return err
	}
	return r.finish()
}

// An accepted message is one event, so it can be shown as an indented block without
// repeating identifiers on every paragraph or interleaving another agent.
func (r *agentRenderer) block(label, kind, text string) error {
	if err := r.finish(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.out, "[%s] %s\n", label, kind); err != nil {
		return err
	}
	text = markdown.Render(text, markdown.Options{Enabled: true, ANSI: false, Width: r.width, Prefix: "  "})
	if _, err := io.WriteString(r.out, text); err != nil {
		return err
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		_, err := fmt.Fprintln(r.out)
		return err
	}
	return nil
}

func (r *agentRenderer) finish() error {
	// A thinking group remains active across newlines until another visible
	// event, another stream, or the end of input closes it.
	r.key = ""
	if !r.open {
		return nil
	}
	r.open = false
	_, err := io.WriteString(r.out, "\n")
	return err
}
