package agent

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

func TestRunJSLocalTimeoutIsolated(t *testing.T) {
	for _, phase := range []string{"execute", "release"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				siblingCode := "export default 42"
				timedCode := "// @noe: {\"timeoutMs\": 10, \"outputBytes\": 2048}\nexport default 0"
				siblingStarted := make(chan context.Context, 1)
				finishSibling := make(chan struct{})
				childJoined := make(chan struct{})
				host := NewHost(Options{
					Models: modelsWithChat(&fixtureProvider{}),
					Execute: func(ctx context.Context, execution Execution) (Result, error) {
						if execution.Code == siblingCode {
							if _, hasDeadline := ctx.Deadline(); hasDeadline {
								t.Error("the sibling without a pragma has a deadline")
							}
							siblingStarted <- ctx
							<-finishSibling
							return Result{Stdout: "sibling output"}, ctx.Err()
						}
						if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) != 10*time.Millisecond {
							t.Errorf("unexpected per-call deadline: %v, %v", deadline, ok)
						}
						if execution.OutputBytes != 2048 {
							t.Errorf("unexpected per-call output budget: %d", execution.OutputBytes)
						}
						h := execution.Operation.host
						h.mu.Lock()
						_, tool := h.admitImageLocked(execution.Operation)
						if tool != nil {
							tool.rawImages = []*imageapi.Data{{Metadata: imageapi.Metadata{ImageID: "tool-image"}}}
						}
						h.mu.Unlock()
						if tool == nil {
							t.Error("tool operation could not attach an image")
						}
						if execution.Operation.beginRequest() {
							go func() {
								defer execution.Operation.endRequest()
								<-ctx.Done()
								time.Sleep(20 * time.Millisecond)
								close(childJoined)
							}()
						} else {
							t.Error("tool operation closed before starting an owned request")
						}
						if phase == "execute" {
							select {
							case <-ctx.Done():
								if ctx.Err() != context.DeadlineExceeded {
									t.Errorf("executor context: %v", ctx.Err())
								}
							case <-time.After(time.Second):
								t.Error("tool context did not expire")
							}
						}
						return Result{Stdout: "timed stdout", Stderr: "timed stderr"}, nil
					},
				})
				defer host.Close()
				op := host.NewOperation(t.Context())
				defer op.Close()
				runID := startFixtureRun(t, op)
				step := &modelStepState{
					id: "step", responseComplete: true,
					toolCalls: map[string]*toolCallState{
						"sibling": {name: "run_js", activityID: "sibling-activity"},
						"timed":   {name: "run_js", activityID: "timed-activity"},
					},
				}
				host.mu.Lock()
				run := host.runs[runID]
				run.modelStep = step
				host.mu.Unlock()

				type toolResult struct {
					response runJSResponse
					err      error
				}
				siblingDone := make(chan toolResult, 1)
				go func() {
					response, err := host.runJS(t.Context(), op, runJSRequest{RunID: runID, ModelStepID: step.id, ToolCallID: "sibling", Code: &siblingCode})
					siblingDone <- toolResult{response, err}
				}()
				var siblingCtx context.Context
				select {
				case siblingCtx = <-siblingStarted:
				case sibling := <-siblingDone:
					t.Fatalf("sibling returned before execution: response=%+v err=%v", sibling.response, sibling.err)
				}
				response, err := host.runJS(t.Context(), op, runJSRequest{RunID: runID, ModelStepID: step.id, ToolCallID: "timed", Code: &timedCode})
				siblingErr := siblingCtx.Err()
				close(finishSibling)
				sibling := <-siblingDone
				if err != nil || response.Reason != "timeout" || response.ExitCode == 0 || response.Stdout != "timed stdout" || response.Stderr != "timed stderr" {
					t.Errorf("timed tool response=%+v err=%v", response, err)
				}
				if len(response.Diagnostics) != 1 || response.Diagnostics[0].Code != "timeout" || response.Diagnostics[0].Stage != "execute" {
					t.Errorf("unexpected timeout diagnostics: %+v", response.Diagnostics)
				}
				if tool := step.toolCalls["timed"]; len(response.Images) != 0 || len(tool.rawImages) != 0 || !tool.imagesReady {
					t.Error("the timed-out tool retained its image attachment")
				}
				select {
				case <-childJoined:
				default:
					t.Error("the timeout result arrived before its operation joined its requests")
				}
				if siblingErr != nil || sibling.err != nil || sibling.response.Reason != "completed" || sibling.response.Stdout != "sibling output" || run.ctx.Err() != nil {
					t.Errorf("sibling response=%+v err=%v, sibling context=%v, run context=%v", sibling.response, sibling.err, siblingErr, run.ctx.Err())
				}
			})
		})
	}
}

func TestRunJSBlockedProgressDoesNotHoldTimedOutTool(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		observerEntered := make(chan struct{})
		releaseObserver := make(chan struct{})
		host := NewHost(Options{
			Models: modelsWithChat(&fixtureProvider{}),
			Observe: func(event events.Event) {
				if event["type"] == "run_js.progress" {
					close(observerEntered)
					<-releaseObserver
				}
			},
			Execute: func(ctx context.Context, execution Execution) (Result, error) {
				execution.Progress("stdout", "working")
				<-ctx.Done()
				return Result{}, ctx.Err()
			},
		})
		defer host.Close()
		op := host.NewOperation(t.Context())
		defer op.Close()
		runID := startFixtureRun(t, op)
		code := `// @noe: {"timeoutMs": 10}`
		done := make(chan struct{})
		go func() {
			defer close(done)
			response, err := host.runJS(t.Context(), op, runJSRequest{RunID: runID, Code: &code})
			if err != nil || response.Reason != "timeout" {
				t.Errorf("tool response=%+v err=%v", response, err)
			}
		}()

		<-observerEntered
		time.Sleep(10 * time.Millisecond)
		synctest.Wait()
		host.mu.Lock()
		active := len(host.runs[runID].childScopes)
		host.mu.Unlock()
		close(releaseObserver)
		<-done
		if active != 0 {
			t.Fatalf("observer kept %d tool scopes open after the deadline", active)
		}
	})
}

func TestRunJSLocalTimeoutDuringSetup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		executed := false
		host := NewHost(Options{
			Models: modelsWithChat(&fixtureProvider{}),
			Observe: func(event events.Event) {
				if event["type"] == "run_js.started" {
					time.Sleep(2 * time.Millisecond)
				}
			},
			Execute: func(context.Context, Execution) (Result, error) {
				executed = true
				return Result{}, nil
			},
		})
		defer host.Close()
		op := host.NewOperation(t.Context())
		defer op.Close()
		runID := startFixtureRun(t, op)
		code := `// @noe: {"timeoutMs": 1}`
		response, err := host.runJS(t.Context(), op, runJSRequest{RunID: runID, Code: &code})
		if executed || err != nil || response.Reason != "timeout" || response.ExitCode == 0 || len(response.Diagnostics) != 1 || response.Diagnostics[0].Stage != "setup" {
			t.Fatalf("executed=%v response=%+v err=%v", executed, response, err)
		}
	})
}

func TestRunJSParentCancellationRemainsAnError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		deadline time.Duration
		wantErr  error
	}{
		{"run deadline during execution", time.Millisecond, context.DeadlineExceeded},
		{"request deadline during release", 15 * time.Millisecond, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				host := NewHost(Options{
					Models: modelsWithChat(&fixtureProvider{}),
					Execute: func(ctx context.Context, execution Execution) (Result, error) {
						if tc.wantErr == context.DeadlineExceeded {
							<-ctx.Done()
							return Result{}, ctx.Err()
						}
						if execution.Operation.beginRequest() {
							go func() {
								defer execution.Operation.endRequest()
								<-ctx.Done()
								time.Sleep(20 * time.Millisecond)
							}()
						} else {
							t.Error("tool operation closed before starting an owned request")
						}
						return Result{}, nil
					},
				})
				defer host.Close()
				parent, cancel := context.WithTimeout(t.Context(), tc.deadline)
				defer cancel()
				opContext, requestContext := t.Context(), t.Context()
				if tc.wantErr == context.DeadlineExceeded {
					opContext = parent
				} else {
					requestContext = parent
				}
				op := host.NewOperation(opContext)
				defer op.Close()
				runID := startFixtureRun(t, op)
				code := `// @noe: {"timeoutMs": 10}`
				response, err := host.runJS(requestContext, op, runJSRequest{RunID: runID, Code: &code})
				if !errors.Is(err, tc.wantErr) || response.Reason != "" {
					t.Fatalf("response=%+v err=%v, want %v", response, err, tc.wantErr)
				}
			})
		})
	}
}
