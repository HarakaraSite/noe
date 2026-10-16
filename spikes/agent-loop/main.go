// agent-loop is an isolated host for experiments with the existing NOE CLI.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

//go:embed lib/*.js examples/*.js fixtures/*.json
var assets embed.FS

type config struct {
	provider, model, keyFile, noe, workdir, traceFile string
	maxSteps, maxTokens                               int
	timeout                                           time.Duration
}

func main() {
	os.Exit(mainCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func mainCLI(args []string, stdout, stderr io.Writer) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	f := flag.NewFlagSet("agent-loop", flag.ContinueOnError)
	f.SetOutput(stderr)
	cfg := config{}
	f.StringVar(&cfg.provider, "provider", "mock", "mock or openrouter")
	f.StringVar(&cfg.model, "model", "deepseek/deepseek-v4.1-flash", "model for all agents")
	f.StringVar(&cfg.keyFile, "key-file", filepath.Join(home, ".config/hjh/credentials/openrouter-api-key"), "host-only API key file")
	f.StringVar(&cfg.noe, "noe", "../../noe", "existing NOE executable")
	f.StringVar(&cfg.workdir, "cwd", ".", "working directory of scripts")
	f.StringVar(&cfg.traceFile, "trace", "", "metadata JSONL output")
	f.IntVar(&cfg.maxSteps, "max-steps", 0, "model turns per agent; 0 means unlimited")
	f.IntVar(&cfg.maxTokens, "max-tokens", 0, "output tokens per model request; 0 uses provider default")
	f.DurationVar(&cfg.timeout, "timeout", 0, "whole invocation timeout; 0 means unlimited")
	f.Usage = func() {
		fmt.Fprintln(stderr, "Usage: agent-loop [flags] demo single|recovery|delegate|service\n       agent-loop [flags] run script.js [args...]")
		f.PrintDefaults()
	}
	if err := f.Parse(args); err != nil {
		return 2
	}
	if cfg.maxSteps < 0 || cfg.maxTokens < 0 || cfg.timeout < 0 {
		fmt.Fprintln(stderr, "limits cannot be negative")
		return 2
	}
	if f.NArg() < 2 || (f.Arg(0) != "demo" && f.Arg(0) != "run") {
		f.Usage()
		return 2
	}
	cfg.noe, err = filepath.Abs(cfg.noe)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cfg.workdir, err = filepath.Abs(cfg.workdir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
		defer cancel()
	}
	var p provider
	switch cfg.provider {
	case "mock":
		p = mockProvider{}
	case "openrouter":
		key, e := os.ReadFile(cfg.keyFile)
		if e != nil {
			fmt.Fprintln(stderr, "read API key:", e)
			return 1
		}
		if strings.TrimSpace(string(key)) == "" {
			fmt.Fprintln(stderr, "API key file is empty")
			return 1
		}
		p = newOpenRouter(strings.TrimSpace(string(key)), cfg.model, cfg.maxTokens)
	default:
		fmt.Fprintln(stderr, "unknown provider:", cfg.provider)
		return 2
	}
	var trace io.Writer = io.Discard
	if cfg.traceFile != "" {
		file, e := os.OpenFile(cfg.traceFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			fmt.Fprintln(stderr, e)
			return 1
		}
		defer file.Close()
		trace = file
	}
	h, err := startHost(ctx, cfg, p, trace)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer h.close()
	var script string
	if f.Arg(0) == "demo" {
		switch f.Arg(1) {
		case "single", "recovery", "delegate", "service":
		default:
			fmt.Fprintln(stderr, "unknown demo:", f.Arg(1))
			return 2
		}
		script = filepath.Join(h.dir, "examples", f.Arg(1)+".js")
	} else {
		script, err = filepath.Abs(f.Arg(1))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	result := h.execute(h.ctx, "root", script, f.Args()[2:])
	fmt.Fprint(stdout, result.Stdout)
	fmt.Fprint(stderr, result.Stderr)
	if result.Reason == "timeout" {
		return 124
	}
	if result.Reason == "cancelled" {
		return 130
	}
	return result.ExitCode
}
