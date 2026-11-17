package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// chain is owned by one sequential experiment/conversation. It deliberately
// does not introduce a registry cache or a production model-service lifetime.
// Callers retain the authoritative full history, including completed tool work.
type chain struct {
	client        openai.Client
	connection    *responses.ResponseConnection
	responseID    string
	history       []item
	configuration [32]byte
	closed        bool
	ConnectMS     float64
	Connections   int
	LastAttempts  []turn
}

func configurationKey(request body) ([32]byte, error) {
	configuration := make(body, len(request))
	for key, value := range request {
		switch key {
		case "input", "previous_response_id", "stream", "background", "tool_choice":
			continue
		}
		configuration[key] = value
	}
	data, err := json.Marshal(configuration)
	return sha256.Sum256(data), err
}

func extends(history, input []item) bool {
	if len(input) <= len(history) {
		return false
	}
	for i := range history {
		if !bytes.Equal(history[i], input[i]) {
			return false
		}
	}
	return true
}

func copyItems(items []item) []item {
	result := make([]item, len(items))
	for i, value := range items {
		result[i] = append(item(nil), value...)
	}
	return result
}

func (c *chain) ensure(ctx context.Context) error {
	if c.closed {
		return errors.New("conversation is closed")
	}
	if c.connection != nil {
		return nil
	}
	connection, elapsed, err := openSocket(ctx, c.client)
	c.ConnectMS += elapsed
	if err != nil {
		return err
	}
	c.Connections++
	c.connection = connection
	return nil
}

func (c *chain) drop() {
	if c.connection != nil {
		c.connection.Abort()
		c.connection = nil
	}
	c.responseID = ""
	c.history = nil
}

func (c *chain) Close() error {
	c.closed = true
	c.responseID = ""
	c.history = nil
	if c.connection == nil {
		return nil
	}
	connection := c.connection
	c.connection = nil
	return connection.Close()
}

// Reconnect explicitly refreshes handshake authentication through the SDK. The
// new store=false connection receives no prior ID and restores no API cache.
func (c *chain) Reconnect(ctx context.Context) error {
	if c.closed {
		return errors.New("conversation is closed")
	}
	if c.connection == nil {
		return c.ensure(ctx)
	}
	start := time.Now()
	connection, err := c.connection.Reconnect(ctx)
	c.ConnectMS += milliseconds(start)
	c.responseID = ""
	c.history = nil
	if err != nil {
		c.drop()
		return err
	}
	c.connection = connection
	c.Connections++
	return nil
}

func (c *chain) Exchange(ctx context.Context, request body) (turn, error) {
	c.LastAttempts = nil
	if err := ctx.Err(); err != nil {
		return turn{}, err
	}
	input, ok := request["input"].([]item)
	if !ok {
		return turn{}, errors.New("spike requires a full input item array")
	}
	configuration, err := configurationKey(request)
	if err != nil {
		return turn{}, err
	}
	if err := c.ensure(ctx); err != nil {
		return turn{}, err
	}
	wire := make(body, len(request))
	for key, value := range request {
		wire[key] = value
	}
	delete(wire, "previous_response_id")
	decision := "new-connection"
	if c.responseID != "" {
		switch {
		case c.configuration != configuration:
			decision = "configuration-changed"
		case !extends(c.history, input):
			decision = "history-replaced"
		default:
			wire["input"] = input[len(c.history):]
			wire["previous_response_id"] = c.responseID
			decision = "append"
		}
	}
	result, err := websocketTurn(ctx, c.connection, wire)
	result.ChainDecision = decision
	var failure *protocolError
	if errors.As(err, &failure) {
		result.FailureCode = failure.Code
	}
	c.LastAttempts = append(c.LastAttempts, result)
	if err != nil {
		_, incremental := wire["previous_response_id"]
		c.drop()
		// This explicit API rejection establishes that generation did not start.
		// Do not replay a send/read failure with uncertain delivery.
		if incremental && failure != nil && failure.Code == "previous_response_not_found" {
			if err := c.ensure(ctx); err != nil {
				return result, err
			}
			delete(wire, "previous_response_id")
			wire["input"] = input
			result, err = websocketTurn(ctx, c.connection, wire)
			result.ChainDecision = "cache-miss-full-replay"
			c.LastAttempts = append(c.LastAttempts, result)
		}
		if err != nil {
			c.drop()
			return result, err
		}
	}
	c.responseID = result.ResponseID
	c.configuration = configuration
	c.history = append(copyItems(input), copyItems(result.Output)...)
	return result, nil
}
