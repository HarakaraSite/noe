package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	publicagent "forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/auth"
	"forge.harakara.site/littleisland/noe/v2/internal/app/control"
	"forge.harakara.site/littleisland/noe/v2/internal/noeprofile"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func newManagedExecution(ctx context.Context, options Options, source Source) (*managedExecution, error) {
	if ctx == nil {
		return nil, errors.New("runtime context is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	success := false
	defer func() {
		if !success {
			cancel()
		}
	}()
	settings := noeprofile.AgentOptions{}
	if options.Agent != nil {
		settings = *options.Agent
	}
	authOptions := auth.Options{}
	if options.Auth != nil {
		authOptions = *options.Auth
	}
	entry, err := filepath.Abs(source.Name)
	if err != nil {
		return nil, err
	}
	profile, err := noeprofile.Profile(noeprofile.ProfileOptions{
		Cwd: options.Cwd, Source: entry, Supervisor: settings.Supervisor, Args: options.Args,
		Stdin: options.Stdin, Stdout: options.Stdout, Stderr: options.Stderr, ImportMap: options.ImportMap, Auth: authOptions,
	})
	if err != nil {
		return nil, err
	}
	modelOptions := settings.Models
	modelOptions.BaseDir = profile.Environment.Cwd
	models := noeprofile.Models(modelOptions)
	taskProfile := profile
	taskProfile.Environment.Stdin = nil
	taskProfile.Environment.Stdout = nil
	taskProfile.Environment.Stderr = nil
	var controller *control.Controller
	var observeMu sync.Mutex
	observe := func(event publicagent.Event) {
		observeMu.Lock()
		defer observeMu.Unlock()
		if settings.Observe != nil {
			settings.Observe(event)
		}
		if event["type"] == "agent.accepted" && controller != nil {
			controller.Accept()
		}
	}
	host, err := publicagent.NewHost(ctx, publicagent.HostConfig{
		Models: models, Runtime: &taskProfile, Instructions: noeprofile.InstructionSource(profile.Environment.Cwd), Policy: noeprofile.Policy(), Observe: observe, Trace: settings.Trace,
	})
	if err != nil {
		return nil, err
	}
	if settings.EnableControl || settings.ControlDir != "" {
		controller = control.New(ctx, control.Options{Host: host, Directory: func() (string, error) {
			if settings.ControlDir != "" {
				return settings.ControlDir, nil
			}
			return control.DefaultControlDir()
		}, Cancel: cancel, Observe: func(event publicagent.Event) {
			observeMu.Lock()
			defer observeMu.Unlock()
			if settings.Observe != nil {
				settings.Observe(event)
			}
		}})
	}
	initialize := profile.Initialize
	profile.Initialize = func(ctx context.Context, view *stdlib.Bindings) (func(), error) {
		closeExtras, err := initialize(ctx, view)
		if err != nil {
			return nil, err
		}
		if view.Worker() {
			return closeExtras, nil
		}
		binding, err := publicagent.Attach(ctx, view, host)
		if err != nil {
			if closeExtras != nil {
				closeExtras()
			}
			return nil, err
		}
		return func() {
			binding.Close()
			if closeExtras != nil {
				closeExtras()
			}
		}, nil
	}
	// Each attachment creates an independent client; a check must close only
	// that client, while keeping the caller's host and client alive.
	profile.InspectInitialize = profile.Initialize
	runtime, err := stdlib.NewRuntime(ctx, profile)
	if err != nil {
		if controller != nil {
			err = errors.Join(err, controller.Close())
		}
		host.Close()
		return nil, err
	}
	success = true
	return &managedExecution{runtime: runtime, host: host, controller: controller, cancel: cancel, source: stdlib.Source{Name: entry, Code: source.Code, Module: true}}, nil
}

type managedExecution struct {
	runtime    *stdlib.Runtime
	host       *publicagent.Host
	controller *control.Controller
	cancel     context.CancelFunc
	source     stdlib.Source
	closed     bool
}

func (e *managedExecution) run() (int, error) {
	result, err := e.runtime.Run(e.source)
	err = errors.Join(err, e.Close())
	if err != nil {
		return 1, err
	}
	return result.Code, nil
}

func (e *managedExecution) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	err := e.runtime.Close()
	if e.controller != nil {
		err = errors.Join(err, e.controller.Close())
	}
	e.cancel()
	e.host.Close()
	return err
}

func executeManaged(ctx context.Context, source Source, options Options) (int, error) {
	execution, err := newManagedExecution(ctx, options, source)
	if err != nil {
		return 1, err
	}
	defer execution.Close()
	return execution.run()
}
