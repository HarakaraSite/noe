package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	publicagent "forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// live is an explicit, bounded smoke test for an operator-selected provider
// configuration. It never prints the key, file contents, prompts, or answer.
func runLive(args []string) error {
	flags := flagSet("live")
	noePath := flags.String("noe", "", "absolute path to an independent NOE executable")
	configPath := flags.String("config", "", "explicit NOE provider configuration file")
	keyPath := flags.String("key-file", "", "explicit OpenCode Go credential file path")
	inputPath := flags.String("input", "", "JSON array of objects with numeric amount fields")
	reportPath := flags.String("report", "", "new or replaceable report.json path beside the input")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("live accepts only --noe, --config, --key-file, --input, and --report")
	}
	if *noePath == "" || !filepath.IsAbs(*noePath) || *configPath == "" || !filepath.IsAbs(*configPath) || *keyPath == "" || !filepath.IsAbs(*keyPath) || *inputPath == "" || *reportPath == "" {
		return errors.New("live requires absolute --noe, --config, and --key-file paths plus --input and --report")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if sameExecutable(self, *noePath) {
		return errors.New("live NOE executable must be separate from this consumer")
	}
	inputPathAbs, err := filepath.Abs(*inputPath)
	if err != nil {
		return errors.New("cannot resolve input path")
	}
	reportPathAbs, err := filepath.Abs(*reportPath)
	if err != nil {
		return errors.New("cannot resolve report path")
	}
	workDir := filepath.Dir(inputPathAbs)
	if filepath.Clean(filepath.Dir(reportPathAbs)) != filepath.Clean(workDir) || filepath.Base(reportPathAbs) == "." || filepath.Base(reportPathAbs) == ".." {
		return errors.New("live report must be a file beside the input in the same workspace")
	}
	if _, err := os.Stat(*configPath); err != nil {
		return errors.New("explicit provider configuration file is unavailable")
	}
	if _, err := os.Stat(*keyPath); err != nil {
		return errors.New("explicit provider credential file is unavailable")
	}
	inputBytes, err := os.ReadFile(inputPathAbs)
	if err != nil {
		return errors.New("cannot read live input file")
	}
	var rows []struct {
		Amount json.Number `json:"amount"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(inputBytes)))
	decoder.UseNumber()
	if err := decoder.Decode(&rows); err != nil || len(rows) == 0 {
		return errors.New("live input must be a nonempty JSON array of objects with numeric amount fields")
	}
	total := 0.0
	for _, row := range rows {
		amount, err := row.Amount.Float64()
		if err != nil {
			return errors.New("live input amount fields must be numeric")
		}
		total += amount
	}

	const credentialEnv = "NOE_OPENCODE_GO_API_KEY_FILE"
	oldKeyPath, hadKeyPath := os.LookupEnv(credentialEnv)
	if err := os.Setenv(credentialEnv, *keyPath); err != nil {
		return errors.New("cannot set the explicit OpenCode Go credential path")
	}
	defer func() {
		if hadKeyPath {
			_ = os.Setenv(credentialEnv, oldKeyPath)
		} else {
			_ = os.Unsetenv(credentialEnv)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 115*time.Second)
	defer cancel()
	var modelCalls, runJSCalls atomic.Int32
	observe := func(event publicagent.Event) {
		switch event["type"] {
		case "model.started":
			modelCalls.Add(1)
		case "run_js.started":
			runJSCalls.Add(1)
		}
	}
	config := publicagent.Config{Providers: publicagent.ProviderOptions{
		ConfigFile: *configPath, KeyFile: *keyPath, UserAgent: "noe-agent-external-consumer-live",
	}, Observe: observe}
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	sink := &borrowedSink{}
	if _, err := stdlib.Install(rt, stdlib.Config{Core: stdlib.CoreConfig{NoWebAPIs: true, Loop: loop, Stdout: sink, Stderr: sink}}); err != nil {
		_ = rt.Close()
		return errors.New("could not install partial stdlib for live consumer")
	}
	binding, err := publicagent.Install(ctx, rt, publicagent.Config{
		Loop: loop, Cwd: workDir, Executable: *noePath, Providers: config.Providers, Observe: observe,
	})
	if err != nil {
		_ = rt.Close()
		return errors.New("could not install public agent package for live consumer")
	}
	defer func() { binding.Close(); _ = rt.Close() }()

	inName, outName := filepath.Base(inputPathAbs), filepath.Base(reportPathAbs)
	task := "Read " + inName + " using JavaScript in run_js. It is a JSON array of records with numeric amount fields. Calculate the record count and sum of amount. Write " + outName + " as JSON with exactly numeric fields count and total. Read the saved report back and verify both values before returning a brief completion answer in English."
	taskJSON, _ := json.Marshal(task)
	source := `import {run_agent} from "noe:agent";
run_agent({provider:"opencode-go",model:"deepseek-v4.1-flash",effort:"auto",maxSteps:6,executionLimits:{providerCalls:6}},` + string(taskJSON) + `)
 .then(value=>{globalThis.liveConsumerOutcome={value};liveConsumerDone();},error=>{globalThis.liveConsumerOutcome={error:"agent task failed"};liveConsumerDone();});`
	taskResult, err := runPromise(ctx, rt, loop, "liveConsumerDone", "fixture-live.mjs", source)
	if err != nil {
		return errors.New("bounded live agent task failed")
	}
	var taskSnapshot struct {
		State      string `json:"state"`
		LastResult struct {
			State  string `json:"state"`
			Answer string `json:"answer"`
		} `json:"lastResult"`
	}
	if err := json.Unmarshal(taskResult, &taskSnapshot); err != nil || taskSnapshot.State != "stopped" || taskSnapshot.LastResult.State != "completed" || strings.TrimSpace(taskSnapshot.LastResult.Answer) == "" {
		return errors.New("live agent task did not finish successfully")
	}
	var report struct {
		Count int     `json:"count"`
		Total float64 `json:"total"`
	}
	data, err := os.ReadFile(reportPathAbs)
	if err != nil || json.Unmarshal(data, &report) != nil {
		return errors.New("live agent did not write a valid JSON report")
	}
	if report.Count != len(rows) || report.Total != total {
		return errors.New("live report did not match independently calculated input totals")
	}
	if modelCalls.Load() == 0 || modelCalls.Load() > 6 || runJSCalls.Load() == 0 {
		return errors.New("live run did not stay within its provider-call limit or use run_js")
	}
	if sink.wasClosed() {
		return errors.New("runtime closed the caller-owned output sink")
	}
	if _, err := sink.Write([]byte("still-open")); err != nil {
		return fmt.Errorf("caller output sink is unusable after live run: %w", err)
	}
	fmt.Printf("live consumer passed: model_requests=%d run_js=%d count=%d total=%g report=%s\n", modelCalls.Load(), runJSCalls.Load(), report.Count, report.Total, reportPathAbs)
	return nil
}
