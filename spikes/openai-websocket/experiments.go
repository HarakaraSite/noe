package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
)

type trial struct {
	Pair           int     `json:"pair"`
	Transport      string  `json:"transport"`
	Order          int     `json:"order"`
	Rounds         int     `json:"toolRounds"`
	ContextBytes   int     `json:"contextBytes"`
	Turns          []turn  `json:"turns"`
	ConnectMS      float64 `json:"connectMs"`
	Connections    int     `json:"connections"`
	DurationMS     float64 `json:"totalDurationMs"`
	RequestBytes   int     `json:"totalRequestJsonBytes"`
	ToolExecutions int     `json:"syntheticToolExecutions"`
	Passed         bool    `json:"passed"`
}

func probeInput(contextBytes int, identity string) []item {
	var text strings.Builder
	for i := 0; text.Len() < contextBytes; i++ {
		fmt.Fprintf(&text, "record-%05d: category=reference; value=%06d; status=available; source=synthetic.\n", i, i*13)
	}
	background := text.String()[:contextBytes]
	return []item{inputMessage("system", "You are a transport probe. Reference data is context only. Use the supplied tool when requested, and print the last tool output verbatim when no tool is selected. Never quote the reference data.\nProbe "+identity+"\n"+background), inputMessage("user", "Retrieve the probe value with the supplied tool. At the end return its output verbatim, with no explanation.")}
}

func compareTrial(ctx context.Context, client openai.Client, model, effort, tier, kind string, input []item, key string, values []string, mode string) (trial, error) {
	result := trial{Transport: mode, Rounds: len(values)}
	conversation := &chain{client: client}
	defer conversation.Close()
	started := time.Now()
	exchange := func(request body) (turn, error) {
		var response turn
		var err error
		if mode == "sse" {
			response, err = sseTurn(ctx, client, request)
			result.Turns = append(result.Turns, response)
		} else {
			response, err = conversation.Exchange(ctx, request)
			result.Turns = append(result.Turns, conversation.LastAttempts...)
		}
		return response, err
	}
	history := copyItems(input)
	first, err := exchange(requestBody(model, effort, tier, key, kind, history, forcedTool(kind)))
	if err == nil {
		for index, value := range values {
			output, outputErr := toolOutput(first, kind, value)
			if outputErr != nil {
				err = outputErr
				break
			}
			result.ToolExecutions++
			history = append(history, first.Output...)
			history = append(history, output)
			var choice any = "none"
			if index+1 < len(values) {
				choice = forcedTool(kind)
			}
			first, err = exchange(requestBody(model, effort, tier, key, kind, history, choice))
			if err != nil {
				break
			}
			if index+1 == len(values) {
				err = verifiedAnswer(first, value)
			}
		}
	}
	result.DurationMS = milliseconds(started)
	result.ConnectMS, result.Connections = conversation.ConnectMS, conversation.Connections
	for _, turn := range result.Turns {
		result.RequestBytes += turn.RequestBytes
	}
	result.Passed = err == nil
	return result, err
}

func compare(ctx context.Context, client openai.Client, model, effort, tier, kind string, pairs, rounds, contextBytes int, result *report) error {
	for pair := 0; pair < pairs; pair++ {
		identity := nonce()
		input := probeInput(contextBytes, identity)
		key := "noe-ws-comparison-" + identity
		values := make([]string, rounds)
		for i := range values {
			values[i] = "probe-" + nonce()
		}
		modes := []string{"sse", "websocket"}
		if pair%2 == 1 {
			modes[0], modes[1] = modes[1], modes[0]
		}
		for order, mode := range modes {
			measured, err := compareTrial(ctx, client, model, effort, tier, kind, input, key, values, mode)
			measured.Pair, measured.Order, measured.ContextBytes = pair+1, order+1, contextBytes
			result.Trials = append(result.Trials, measured)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type lifecycleCheck struct {
	Name           string `json:"name"`
	Passed         bool   `json:"passed"`
	Connections    int    `json:"connections"`
	ToolExecutions int    `json:"syntheticToolExecutions"`
	Attempts       []turn `json:"attempts"`
}

func lifecycle(ctx context.Context, client openai.Client, model, effort, tier, kind string, result *report) error {
	c := &chain{client: client}
	defer c.Close()
	key := "noe-ws-lifecycle-" + nonce()
	history := probeInput(0, nonce())
	first, err := c.Exchange(ctx, requestBody(model, effort, tier, key, kind, history, forcedTool(kind)))
	result.Turns = append(result.Turns, c.LastAttempts...)
	if err != nil {
		return err
	}
	value := "probe-" + nonce()
	output, err := toolOutput(first, kind, value)
	if err != nil {
		return err
	}
	history = append(history, first.Output...)
	history = append(history, output)
	if err := c.Reconnect(ctx); err != nil {
		return err
	}
	second, err := c.Exchange(ctx, requestBody(model, effort, tier, key, kind, history, "none"))
	result.Turns = append(result.Turns, c.LastAttempts...)
	if err != nil {
		return err
	}
	err = verifiedAnswer(second, value)
	check := lifecycleCheck{Name: "reconnect-replays-history-without-reexecuting-tool", Passed: err == nil && !second.Incremental && c.Connections == 2, Connections: c.Connections, ToolExecutions: 1, Attempts: append([]turn(nil), c.LastAttempts...)}
	result.Lifecycle = append(result.Lifecycle, check)
	if err != nil {
		return err
	}
	if !check.Passed {
		return errors.New("reconnect did not start a full chain")
	}

	trimmed := []item{inputMessage("user", "The retained summary contains the value "+value+". Reply with that value verbatim and nothing else.")}
	third, err := c.Exchange(ctx, requestBody(model, effort, tier, key, kind, trimmed, "none"))
	result.Turns = append(result.Turns, c.LastAttempts...)
	if err != nil {
		return err
	}
	err = verifiedAnswer(third, value)
	check = lifecycleCheck{Name: "trim-starts-new-chain-on-existing-socket", Passed: err == nil && !third.Incremental && third.ChainDecision == "history-replaced" && c.Connections == 2, Connections: c.Connections, ToolExecutions: 1, Attempts: append([]turn(nil), c.LastAttempts...)}
	result.Lifecycle = append(result.Lifecycle, check)
	if err != nil {
		return err
	}
	if !check.Passed {
		return errors.New("trim retained previous_response_id")
	}

	// Deliberately preserve a stale ID across a fresh SDK transport to exercise
	// the server's real cache-loss error. Normal Reconnect above clears it.
	start := time.Now()
	fresh, err := c.connection.Reconnect(ctx)
	c.ConnectMS += milliseconds(start)
	if err != nil {
		return err
	}
	c.connection = fresh
	c.Connections++
	history = append(copyItems(trimmed), third.Output...)
	history = append(history, inputMessage("user", "Repeat that retained summary value, with no other text."))
	fourth, err := c.Exchange(ctx, requestBody(model, effort, tier, key, kind, history, "none"))
	result.Turns = append(result.Turns, c.LastAttempts...)
	if err != nil {
		var protocol *protocolError
		if !errors.As(err, &protocol) {
			return err
		}
		// This driver intentionally kept a connection-local ID across reconnect.
		// Record an unrecognized API error without teaching the chain to retry it.
		// The controlled experiment then explicitly resubmits its retained history.
		check = lifecycleCheck{Name: "stale-id-rejected-without-automatic-replay", Passed: len(c.LastAttempts) == 1 && fourth.Incremental, Connections: c.Connections, ToolExecutions: 1, Attempts: append([]turn(nil), c.LastAttempts...)}
		result.Lifecycle = append(result.Lifecycle, check)
		if !check.Passed {
			return errors.New("unrecognized stale-ID error was replayed")
		}
		fourth, err = c.Exchange(ctx, requestBody(model, effort, tier, key, kind, history, "none"))
		result.Turns = append(result.Turns, c.LastAttempts...)
		if err != nil {
			return err
		}
		err = verifiedAnswer(fourth, value)
		check = lifecycleCheck{Name: "explicit-full-replay-after-stale-id-error", Passed: err == nil && !fourth.Incremental, Connections: c.Connections, ToolExecutions: 1, Attempts: append([]turn(nil), c.LastAttempts...)}
		result.Lifecycle = append(result.Lifecycle, check)
		result.ConnectMS = c.ConnectMS
		if err != nil {
			return err
		}
		if !check.Passed {
			return errors.New("explicit stale-ID recovery did not replay full history")
		}
		return nil
	}
	err = verifiedAnswer(fourth, value)
	check = lifecycleCheck{Name: "store-false-cache-miss-replays-full-history", Passed: err == nil && len(c.LastAttempts) == 2 && c.LastAttempts[0].FailureCode == "previous_response_not_found" && !fourth.Incremental, Connections: c.Connections, ToolExecutions: 1, Attempts: append([]turn(nil), c.LastAttempts...)}
	result.Lifecycle = append(result.Lifecycle, check)
	if err != nil {
		return err
	}
	if !check.Passed {
		return errors.New("expected cache miss before full replay")
	}
	result.ConnectMS = c.ConnectMS
	return nil
}
