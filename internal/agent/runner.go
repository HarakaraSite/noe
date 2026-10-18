package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	processrunner "noe/internal/agent/runner"
	"noe/internal/modulemap"
)

// Runner starts a fresh NOE process for each JavaScript execution.
type Runner struct {
	Executable string
	Cwd        string
	Connection *Connection
	ImportMap  *modulemap.Snapshot
}

// Result is the observable outcome of a child NOE process. A non-zero exit,
// timeout, or cancellation is returned as a result; error is reserved for
// setup and supervision failures.
type Result = processrunner.Result

const childSourcePrelude = "import { run_agent, spawn_agent, observe_agent, queue_agent, steer_agent, interrupt_agent } from \"noe:agent\";\n"

// Run prepends the public agent helper import used by run_js code.
func (r Runner) Run(ctx context.Context, code string) (Result, error) {
	return r.runSource(ctx, childSourcePrelude+code)
}

// RunTask starts the private model/tool loop directly for one managed task.
// Config is a JSON object with agent settings; it and instruction are encoded
// as JSON strings before being placed in JavaScript source.
func (r Runner) RunTask(ctx context.Context, config json.RawMessage, instruction string) (Result, error) {
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	if !json.Valid(config) {
		return failedResult(), errors.New("task config must contain valid JSON")
	}
	configJSON, err := json.Marshal(string(config))
	if err != nil {
		return failedResult(), fmt.Errorf("encode task config: %w", err)
	}
	instructionJSON, err := json.Marshal(instruction)
	if err != nil {
		return failedResult(), fmt.Errorf("encode task instruction: %w", err)
	}
	bootstrap := "import { executeTask } from \"noe:agent/task\";\n" +
		"const config = JSON.parse(" + string(configJSON) + ");\n" +
		"const instruction = " + string(instructionJSON) + ";\n" +
		"try { await executeTask(config, instruction); } catch (error) { console.error(error); }\n"
	return r.runSource(ctx, bootstrap)
}

// runSource writes source to a private temporary module, starts a fresh NOE
// process, captures both output streams, and terminates the process scope after
// the root process exits. The caller context is the only timeout source.
func (r Runner) runSource(ctx context.Context, source string) (result Result, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return Result{ExitCode: -1, Reason: reasonForContext(ctx.Err())}, nil
	}
	if r.Executable == "" {
		return failedResult(), errors.New("runner executable is required")
	}
	executable, err := filepath.Abs(r.Executable)
	if err != nil {
		return failedResult(), fmt.Errorf("resolve runner executable: %w", err)
	}
	cwd := r.Cwd
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return failedResult(), fmt.Errorf("resolve runner working directory: %w", err)
		}
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return failedResult(), fmt.Errorf("resolve runner working directory: %w", err)
	}
	if r.Connection != nil {
		if r.Connection.URL == "" || r.Connection.Token == "" || r.Connection.Scope == "" {
			return failedResult(), &RPCError{Code: "connection", Message: "agent connection is incomplete"}
		}
		if err := validateConnectionURL(r.Connection.URL); err != nil {
			return failedResult(), err
		}
	}

	tempBase := sourceTempDirBase(cwd, os.TempDir())
	tempDir, err := os.MkdirTemp(tempBase, "noe-agent-run-")
	if err != nil {
		if tempBase != "" {
			return failedResult(), fmt.Errorf("create private agent source directory on working-directory volume %q: %w", cwd, err)
		}
		return failedResult(), fmt.Errorf("create private agent source directory: %w", err)
	}
	defer func() {
		retErr = errors.Join(retErr, os.RemoveAll(tempDir))
	}()
	sourcePath := filepath.Join(tempDir, "agent-run.js")
	if err := os.WriteFile(sourcePath, []byte(source), 0600); err != nil {
		return failedResult(), fmt.Errorf("write private agent source: %w", err)
	}
	args := []string{"run"}
	if r.ImportMap != nil {
		snapshot, err := json.Marshal(r.ImportMap)
		if err != nil {
			return failedResult(), fmt.Errorf("encode import map snapshot: %w", err)
		}
		mapPath := filepath.Join(tempDir, "import-map.json")
		if err := os.WriteFile(mapPath, snapshot, 0600); err != nil {
			return failedResult(), fmt.Errorf("write private import map snapshot: %w", err)
		}
		args = append(args, "--import-map", mapPath)
	}
	args = append(args, sourcePath)
	env, err := childEnvironment(os.Environ(), r.Connection)
	if err != nil {
		return failedResult(), fmt.Errorf("prepare agent child environment: %w", err)
	}
	if ctx.Err() != nil {
		return Result{ExitCode: -1, Reason: reasonForContext(ctx.Err())}, nil
	}

	return processrunner.Run(ctx, processrunner.Command{
		Executable: executable,
		Args:       args,
		Cwd:        cwd,
		Env:        env,
	})
}

func failedResult() Result {
	return Result{ExitCode: -1, Reason: "failed"}
}

func reasonForContext(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if err != nil {
		return "cancelled"
	}
	return ""
}

// sourceTempDirBase keeps the generated module on the working-directory
// volume when the system temp directory is on another volume. NOE resolves
// relative module imports from the generated source path, so this preserves
// access to files below cwd on Windows. An empty base retains os.MkdirTemp's
// default system temp location.
func sourceTempDirBase(cwd, systemTemp string) string {
	cwdVolume := filepath.VolumeName(cwd)
	tempVolume := filepath.VolumeName(systemTemp)
	if cwdVolume != "" && tempVolume != "" && !strings.EqualFold(cwdVolume, tempVolume) {
		return cwd
	}
	return ""
}

func childEnvironment(parent []string, connection *Connection) ([]string, error) {
	var encoded []byte
	var err error
	if connection != nil {
		encoded, err = json.Marshal(connection)
		if err != nil {
			return nil, err
		}
	}
	env := make([]string, 0, len(parent)+1)
	for _, item := range parent {
		key, _, ok := strings.Cut(item, "=")
		if !ok || key == "" || isSensitiveChildEnv(key) {
			continue
		}
		env = append(env, item)
	}
	if connection != nil {
		env = append(env, "NOE_AGENT_CONNECTION="+string(encoded))
	}
	return env, nil
}

func isSensitiveChildEnv(key string) bool {
	if sameEnvironmentKey(key, "NOE_AGENT_CONNECTION") {
		return true
	}
	for _, name := range []string{
		"OPENROUTER_API_KEY",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"NOE_OPENROUTER_API_KEY",
	} {
		if sameEnvironmentKey(key, name) {
			return true
		}
	}
	return false
}
