//go:build linux || darwin

package nativeio

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

const signalWorkerHelperEnv = "NOE_NATIVEIO_SIGNAL_WORKER_HELPER"

// TestSignalsReachOnlyTheRuntimeAndNameThatSubscribed runs in a child so real
// process signals cannot change the test runner's own handling.
func TestSignalsReachOnlyTheRuntimeAndNameThatSubscribed(t *testing.T) {
	if os.Getenv(signalWorkerHelperEnv) == "1" {
		runSignalWorkerHelper(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestSignalsReachOnlyTheRuntimeAndNameThatSubscribed$")
	cmd.Env = append(os.Environ(), signalWorkerHelperEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	t.Cleanup(func() {
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	eventChannel := make(chan string, 32)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "JS_EVENT:") || strings.HasPrefix(line, "GO_EVENT:") {
				eventChannel <- line
			}
		}
	}()
	events := signalEventQueue{events: eventChannel}

	events.wait(t, "JS_EVENT:worker-ready")
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM to helper: %v", err)
	}
	events.wait(t, "JS_EVENT:worker-signal:SIGTERM")
	events.wait(t, "JS_EVENT:worker-closed")
	events.wait(t, "JS_EVENT:worker-terminated")
	events.wait(t, "JS_EVENT:worker-exit")
	events.assertNo(t, "JS_EVENT:main-signal:SIGTERM")

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send SIGINT to helper: %v", err)
	}
	events.wait(t, "JS_EVENT:main-signal:SIGINT")
	events.wait(t, "GO_EVENT:notify-context-canceled")
	events.wait(t, "GO_EVENT:loop-returned")
	if err := cmd.Wait(); err != nil {
		t.Fatalf("signal helper: %v; stderr: %s", err, stderr.String())
	}
	finished = true
}

func runSignalWorkerHelper(t *testing.T) {
	writeEvent := func(event string) { fmt.Fprintln(os.Stdout, event) }
	ctx, stopNotify := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopNotify()
	contextCanceled := make(chan struct{})
	go func() {
		<-ctx.Done()
		writeEvent("GO_EVENT:notify-context-canceled")
		close(contextCanceled)
	}()

	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	workers := &stdlib.Workers{
		New: func() (*quickjs.Runtime, error) { return quickjs.New(), nil },
		Installed: func(workerRT *quickjs.Runtime) error {
			_, err := Install(workerRT, Options{Stdout: os.Stdout, Stderr: os.Stderr})
			return err
		},
		Max: -1,
	}
	if err := stdlib.Install(rt, stdlib.Config{Loop: loop, Workers: workers, Stdout: os.Stdout, Stderr: os.Stderr}); err != nil {
		t.Fatalf("install stdlib: %v", err)
	}
	if _, err := Install(rt, Options{Stdout: os.Stdout, Stderr: os.Stderr}); err != nil {
		t.Fatalf("install native host: %v", err)
	}
	_, err := rt.EvalModule("signal-worker-helper.mjs", `
import {Worker} from "node:worker_threads";
import {subscribe} from "noe:signals";

// Both subscriptions name SIGINT. Closing one must leave the other alive.
const removed = subscribe("SIGINT");
const main = subscribe("SIGINT");
removed.close();
main.next().then(async ({value, done}) => {
  if (!done) console.log("JS_EVENT:main-signal:" + value);
  await main.close();
});

const workerSource = `+"`"+`
  import {parentPort} from "node:worker_threads";
  import {subscribe} from "noe:signals";
  const signals = subscribe("SIGTERM");
  // Share the main runtime's SIGINT registration until the parent terminates
  // this Worker, then verify the main subscription survives its removal.
  const shared = subscribe("SIGINT");
  parentPort.postMessage("worker-ready");
  (async () => {
    for await (const value of signals) {
      parentPort.postMessage("worker-signal:" + value);
      break;
    }
    parentPort.postMessage("worker-closed");
    await shared.next();
  })();
`+"`"+`;
const worker = new Worker(workerSource, {eval:true, type:"module"});
worker.on("message", async message => {
  console.log("JS_EVENT:" + message);
  if (message === "worker-closed") {
    await worker.terminate();
    console.log("JS_EVENT:worker-terminated");
  }
});
worker.on("exit", () => console.log("JS_EVENT:worker-exit"));
console.log("JS_EVENT:main-ready");
`)
	if err != nil {
		t.Fatalf("evaluate signal helper: %v", err)
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("signal helper loop: %v", err)
	}
	<-ctx.Done()
	<-contextCanceled
	writeEvent("GO_EVENT:loop-returned")
	_ = rt.Close()
}

type signalEventQueue struct {
	events <-chan string
	queued []string
}

func (q *signalEventQueue) wait(t *testing.T, want string) {
	t.Helper()
	for i, event := range q.queued {
		if event == want {
			q.queued = append(q.queued[:i], q.queued[i+1:]...)
			return
		}
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-q.events:
			if event == want {
				return
			}
			q.queued = append(q.queued, event)
		case <-deadline.C:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}

func (q *signalEventQueue) assertNo(t *testing.T, unexpected string) {
	t.Helper()
	for _, event := range q.queued {
		if event == unexpected {
			t.Fatalf("unexpected signal delivery %q", event)
		}
	}
	select {
	case event := <-q.events:
		if event == unexpected {
			t.Fatalf("unexpected signal delivery %q", event)
		}
		q.queued = append(q.queued, event)
	case <-time.After(30 * time.Millisecond):
	}
}
