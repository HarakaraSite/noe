package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	agentobservation "forge.harakara.site/littleisland/noe/v2/internal/observation"
)

func runObserve(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("noe observe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	runID := flags.String("run", "", "select a run ID")
	follow := flags.Bool("follow", false, "follow appended records in a file")
	structured := flags.Bool("json", false, "return a structured run snapshot")
	wait := flags.Bool("wait", false, "with --json, wait for the selected run to finish")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(stderr, "noe observe: expected at most one events file")
		return 2
	}
	path := "-"
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	if *structured {
		if path == "-" || *runID == "" || *follow {
			fmt.Fprintln(stderr, "noe observe: --json requires --run and an events file; --follow is for terminal display")
			return 2
		}
		snapshot, err := agentobservation.Observe(ctx, agentobservation.Query{LogPath: path, RunID: *runID, Wait: *wait})
		if err == nil {
			err = json.NewEncoder(stdout).Encode(snapshot)
		}
		if err != nil {
			fmt.Fprintln(stderr, "noe observe:", err)
			if ctx.Err() != nil {
				return 130
			}
			return 1
		}
		return 0
	}
	if *wait {
		fmt.Fprintln(stderr, "noe observe: --wait is used with --json")
		return 2
	}
	var input io.Reader = stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			fmt.Fprintln(stderr, "noe observe:", err)
			return 1
		}
		defer file.Close()
		input = file
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type item struct {
		event events.Event
		err   error
	}
	items := make(chan item, 8)
	go func() {
		defer close(items)
		err := readObservationEvents(ctx, input, *follow && path != "-", func(event events.Event) error {
			select {
			case items <- item{event: event}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if err != nil {
			select {
			case items <- item{err: err}:
			case <-ctx.Done():
			}
		}
	}()
	renderer := newAgentRenderer(stdout)
	for {
		select {
		case <-ctx.Done():
			_ = renderer.finish()
			return 130
		case entry, ok := <-items:
			if !ok {
				if err := renderer.finish(); err != nil {
					fmt.Fprintln(stderr, "noe observe:", err)
					return 1
				}
				return 0
			}
			if entry.err != nil {
				_ = renderer.finish()
				fmt.Fprintln(stderr, "noe observe:", entry.err)
				return 1
			}
			if *runID != "" && entry.event["runId"] != *runID {
				continue
			}
			if err := renderer.render(entry.event); err != nil {
				fmt.Fprintln(stderr, "noe observe:", err)
				return 1
			}
		}
	}
}

// A file follower retains a partial row across EOF. Stdin ends at EOF, while
// files can gain more bytes. ReadBytes avoids Scanner's default line-size limit.
func readObservationEvents(ctx context.Context, input io.Reader, follow bool, emit func(events.Event) error) error {
	reader := bufio.NewReader(input)
	var pending []byte
	line := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		part, readErr := reader.ReadBytes('\n')
		pending = append(pending, part...)
		if len(pending) > 0 && pending[len(pending)-1] == '\n' {
			line++
			if strings.TrimSpace(string(pending)) != "" {
				var event events.Event
				if err := json.Unmarshal(pending, &event); err != nil {
					return fmt.Errorf("event line %d: %w", line, err)
				}
				if event == nil || event["type"] == nil {
					return fmt.Errorf("event line %d: missing type", line)
				}
				if err := emit(event); err != nil {
					return err
				}
			}
			pending = nil
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return readErr
			}
			if !follow {
				return nil
			}
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}
