package nativeio

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"noe/internal/process"

	quickjs "github.com/go-quickjs/go-quickjs"
)

type spawnRequest struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
	Stdin   string            `json:"stdin"`
	Stdout  string            `json:"stdout"`
	Stderr  string            `json:"stderr"`
}

func (h *Host) spawn(payload string) *quickjs.Promise {
	var request spawnRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		p := h.rt.NewPromise()
		p.RejectError(err)
		return p
	}
	dir := request.Cwd
	if dir == "" {
		dir = h.opts.Cwd
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(h.opts.Cwd, dir)
	}
	env := make([]string, 0, len(request.Env))
	for k, v := range request.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	return h.async(func() (any, error) {
		return process.Start(h.ctx, process.Config{Command: request.Command, Args: request.Args, Dir: dir, Env: env})
	}, func(value any) (any, error) {
		return h.childObject(value.(*process.Child), request)
	}, func(value any) {
		if c, ok := value.(*process.Child); ok && c != nil {
			_ = c.Close()
		}
	})
}

type childState struct {
	h         *Host
	child     *process.Child
	endpoints []*endpoint
	pumps     sync.WaitGroup
	once      sync.Once
	closeErr  error
	mu        sync.Mutex
	pumpErr   error
}

func (s *childState) Close() error {
	s.once.Do(func() {
		// Cancel borrowed pumps and close owned pipes before waiting for any
		// process or I/O completion. No JS callback is needed for recovery.
		for _, e := range s.endpoints {
			_ = e.Close()
		}
		s.closeErr = s.child.Close()
		s.pumps.Wait()
		s.mu.Lock()
		s.closeErr = errors.Join(s.closeErr, s.pumpErr)
		s.mu.Unlock()
		s.h.release(s)
	})
	return s.closeErr
}

func (s *childState) pump(src, dst *endpoint) {
	s.pumps.Add(1)
	go func() {
		defer s.pumps.Done()
		defer src.Close()
		defer dst.Close()
		buf := make([]byte, 64<<10)
		for {
			n, err := src.readBytes(buf)
			if n > 0 {
				written, writeErr := dst.writeBytes(buf[:n])
				if writeErr != nil || written != n {
					if writeErr == nil {
						writeErr = io.ErrShortWrite
					}
					s.recordPumpError(writeErr, src, dst)
					return
				}
			}
			if err != nil {
				s.recordPumpError(err, src, dst)
				return
			}
		}
	}()
}

func (s *childState) recordPumpError(err error, src, dst *endpoint) {
	if errors.Is(err, io.EOF) || src.ctx.Err() != nil || dst.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	s.pumpErr = errors.Join(s.pumpErr, err)
	s.mu.Unlock()
}

func (h *Host) childObject(child *process.Child, request spawnRequest) (quickjs.Value, error) {
	s := &childState{h: h, child: child}
	h.own(s)
	o := h.rt.NewObject()
	if err := o.Set("pid", child.PID); err != nil {
		_ = s.Close()
		return quickjs.Value{}, err
	}
	var inputPump *endpoint
	for _, part := range []struct {
		name, mode string
		file       *os.File
		input      bool
		host       io.Writer
	}{
		{"stdin", request.Stdin, child.Stdin, true, nil},
		{"stdout", request.Stdout, child.Stdout, false, h.opts.Stdout},
		{"stderr", request.Stderr, child.Stderr, false, h.opts.Stderr},
	} {
		var endpoint *endpoint
		if part.input {
			endpoint = h.endpoint(nil, part.file, part.file)
		} else {
			endpoint = h.endpoint(part.file, nil, part.file)
		}
		s.endpoints = append(s.endpoints, endpoint)
		if part.mode == "pipe" {
			if err := o.Set(part.name, endpoint.object()); err != nil {
				_ = s.Close()
				return quickjs.Value{}, err
			}
			continue
		}
		_ = o.Set(part.name, nil)
		if part.input {
			if part.mode == "ignore" {
				_ = endpoint.Close()
			} else {
				inputPump = h.endpoint(h.opts.Stdin, nil, nil)
				s.endpoints = append(s.endpoints, inputPump)
				s.pump(inputPump, endpoint)
			}
		} else {
			out := part.host
			if part.mode == "ignore" {
				out = io.Discard
			}
			dst := h.endpoint(nil, out, nil)
			s.endpoints = append(s.endpoints, dst)
			s.pump(endpoint, dst)
		}
	}
	exited := h.rt.NewPromise()
	if err := errors.Join(
		o.Set("exited", exited),
		o.Set("kill", func(signal string) *quickjs.Promise {
			return h.async(func() (any, error) { return nil, child.Kill(signal) }, nil, nil)
		}),
		o.Set("close", func() *quickjs.Promise { return h.async(func() (any, error) { return nil, s.Close() }, nil, nil) }),
	); err != nil {
		_ = s.Close()
		return quickjs.Value{}, err
	}
	w := h.rt.StartAsyncWork()
	h.active.Add(1)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		result, err := child.Wait()
		if inputPump != nil {
			_ = inputPump.Close()
		}
		w.Post(func(closed error) {
			if closed != nil {
				return
			}
			if err != nil {
				exited.RejectError(err)
				return
			}
			var code any
			if result.Code != nil {
				code = *result.Code
			}
			var signal any
			if result.Signal != "" {
				signal = result.Signal
			}
			_ = exited.Resolve(map[string]any{"code": code, "signal": signal})
		})
		s.pumps.Wait()
		w.Complete(func(error) { h.active.Add(-1) })
	}()
	return o, nil
}
