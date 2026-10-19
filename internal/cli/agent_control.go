package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
)

func runAgentControl(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(stderr, "noe:", err)
		if errors.Is(err, context.Canceled) {
			return 130
		}
		return 1
	}
	invalid := func() int { fmt.Fprint(stderr, usage); return 2 }
	if len(args) == 0 {
		return invalid()
	}
	switch args[0] {
	case "agents":
		if len(args) != 1 {
			return invalid()
		}
	case "steer", "queue":
		if len(args) < 3 || strings.TrimSpace(strings.Join(args[2:], " ")) == "" {
			return invalid()
		}
	case "interrupt":
		if len(args) != 3 || (args[1] != "--agent" && args[1] != "--run") {
			return invalid()
		}
	default:
		return invalid()
	}
	dir, err := agent.DefaultControlDir()
	if err != nil {
		return fail(err)
	}
	if args[0] == "agents" {
		hosts, err := agent.DiscoverControls(ctx, dir)
		if err != nil {
			return fail(err)
		}
		count := 0
		for _, host := range hosts {
			count += len(host.Listing.Agents)
		}
		if count == 0 {
			_, err := fmt.Fprintln(stdout, "No active agents.")
			if err != nil {
				return fail(err)
			}
			return 0
		}
		writer := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "HOST\tAGENT ID\tRUN ID\tSTATE\tQUEUED\tNAME")
		for _, host := range hosts {
			for _, snapshot := range host.Listing.Agents {
				run := snapshot.CurrentRunID
				if run == "" {
					run = "-"
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d\t%s\n", shortID(host.Listing.HostID), snapshot.AgentID, run, snapshot.State, snapshot.QueueLength, snapshot.Name)
			}
		}
		if err := writer.Flush(); err != nil {
			return fail(err)
		}
		return 0
	}
	id := args[1]
	run := args[0] == "steer"
	if args[0] == "interrupt" {
		id = args[2]
		run = args[1] == "--run"
	}
	connection, fullID, err := agent.ResolveControlTarget(ctx, dir, id, run)
	if err != nil {
		return fail(err)
	}
	payload := make(map[string]any)
	if run {
		payload["runId"] = fullID
	} else {
		payload["agentId"] = fullID
	}
	if args[0] != "interrupt" {
		payload["instruction"] = strings.Join(args[2:], " ")
	}
	result, err := connection.Call(ctx, args[0], payload)
	if err != nil {
		return fail(err)
	}
	var response struct{ Status, CommandID string }
	if err := json.Unmarshal(result, &response); err != nil {
		return fail(err)
	}
	if response.Status == "" {
		return fail(errors.New("agent host returned no command status"))
	}
	identifier := response.CommandID
	if identifier == "" {
		identifier = fullID
	}
	if _, err := fmt.Fprintf(stdout, "%s %s: %s\n", args[0], response.Status, identifier); err != nil {
		return fail(err)
	}
	return 0
}
