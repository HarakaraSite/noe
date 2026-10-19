// Package nativeio installs NOE-owned I/O and signal extensions.
// It uses the pinned standard library's public Streams and Worker hooks.
package nativeio

import (
	"context"
	_ "embed"
	"errors"
	"io"
	"os"
	"sync"

	"noe/internal/hostio"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed js/streams.js
var streamsSource string

type Options struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Cwd            string
}

type Host struct {
	rt        *quickjs.Runtime
	ctx       context.Context
	cancel    context.CancelFunc
	opts      Options
	mu        sync.Mutex
	closed    bool
	resources map[io.Closer]struct{}
	wg        sync.WaitGroup
}

// Install must run on the runtime owner, after stdlib.Install.
func Install(rt *quickjs.Runtime, opts Options) (result *Host, err error) {
	ctx, cancel := context.WithCancel(rt.Context())
	h := &Host{rt: rt, ctx: ctx, cancel: cancel, opts: opts, resources: make(map[io.Closer]struct{})}
	rt.OnClose(h.Close)
	defer func() {
		if err != nil {
			h.Close()
		}
	}()
	input := h.endpoint(opts.Stdin, nil, nil)
	output := h.endpoint(nil, opts.Stdout, nil)
	errout := h.endpoint(nil, opts.Stderr, nil)
	api := rt.NewObject()
	if err := errors.Join(api.Set("stdin", input.object()), api.Set("stdout", output.object()), api.Set("stderr", errout.object()), api.Set("spawn", h.spawn)); err != nil {
		h.Close()
		return nil, err
	}
	factory, err := rt.EvalFile("<noe-streams>", streamsSource)
	if err != nil {
		h.Close()
		return nil, err
	}
	values, err := factory.Call(api)
	if err != nil {
		h.Close()
		return nil, err
	}
	exports := make(map[string]quickjs.Value)
	for _, name := range []string{"stdin", "stdout", "stderr", "pipe"} {
		v, e := values.Get(name)
		if e != nil {
			return nil, e
		}
		exports[name] = v
	}
	if err := rt.SetModuleValues("noe:io", exports); err != nil {
		return nil, err
	}
	spawn, e := values.Get("spawn")
	if e != nil {
		return nil, e
	}
	if err := rt.SetModuleValues("noe:process", map[string]quickjs.Value{"spawn": spawn}); err != nil {
		return nil, err
	}
	for _, install := range []func() error{h.installFS, h.installNet, h.installSignals} {
		if err := install(); err != nil {
			h.Close()
			return nil, err
		}
	}
	return h, nil
}

// Close touches only Go resources. It never waits for JS promise delivery.
func (h *Host) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	h.cancel()
	resources := make([]io.Closer, 0, len(h.resources))
	for c := range h.resources {
		resources = append(resources, c)
	}
	h.mu.Unlock()
	for _, c := range resources {
		_ = c.Close()
	}
	h.wg.Wait()
}

func (h *Host) own(c io.Closer) bool {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = c.Close()
		return false
	}
	h.resources[c] = struct{}{}
	h.mu.Unlock()
	return true
}
func (h *Host) release(c io.Closer) {
	h.mu.Lock()
	delete(h.resources, c)
	h.mu.Unlock()
}

// async snapshots all JS inputs before leaving the owner. convert, if any,
// also runs only on the owner; drop releases results after runtime shutdown.
func (h *Host) async(run func() (any, error), convert func(any) (any, error), drop func(any)) *quickjs.Promise {
	p := h.rt.NewPromise()
	w := h.rt.StartAsyncWork()
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		value, err := run()
		w.Complete(func(closed error) {
			if closed != nil {
				if drop != nil {
					drop(value)
				}
				return
			}
			if err == nil && convert != nil {
				value, err = convert(value)
			}
			if err != nil {
				p.RejectError(err)
			} else if e := p.Resolve(value); e != nil {
				p.RejectError(e)
			}
		})
	}()
	return p
}

type endpoint struct {
	h            *Host
	ctx          context.Context
	cancel       context.CancelFunc
	reader       io.Reader
	writer       io.Writer
	owned        *os.File
	mu           sync.Mutex
	closed       bool
	readAdapter  hostio.Reader
	writeAdapter hostio.Writer
}

func (h *Host) endpoint(r io.Reader, w io.Writer, owned *os.File) *endpoint {
	ctx, cancel := context.WithCancel(h.ctx)
	e := &endpoint{h: h, ctx: ctx, cancel: cancel, reader: r, writer: w, owned: owned}
	h.own(e)
	return e
}

func (e *endpoint) object() quickjs.Value {
	o := e.h.rt.NewObject()
	_ = o.Set("read", e.read)
	_ = o.Set("write", e.write)
	_ = o.Set("close", func() error { return e.Close() })
	return o
}

func (e *endpoint) adapters() (hostio.Reader, hostio.Writer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, nil, os.ErrClosed
	}
	if e.reader != nil && e.readAdapter == nil && e.owned == nil {
		r, err := hostio.NewReader(e.reader)
		if err != nil {
			return nil, nil, err
		}
		e.readAdapter = r
	}
	if e.writer != nil && e.writeAdapter == nil && e.owned == nil {
		w, err := hostio.NewWriter(e.writer)
		if err != nil {
			return nil, nil, err
		}
		e.writeAdapter = w
	}
	return e.readAdapter, e.writeAdapter, nil
}

func (e *endpoint) readBytes(p []byte) (int, error) {
	r, _, err := e.adapters()
	if err != nil {
		return 0, err
	}
	if err := e.ctx.Err(); err != nil {
		return 0, err
	}
	if e.reader == nil {
		return 0, io.EOF
	}
	if e.owned != nil {
		return e.reader.Read(p)
	}
	return r.ReadContext(e.ctx, p)
}

func (e *endpoint) writeBytes(p []byte) (int, error) {
	_, w, err := e.adapters()
	if err != nil {
		return 0, err
	}
	if err := e.ctx.Err(); err != nil {
		return 0, err
	}
	if e.writer == nil {
		return len(p), nil
	}
	if e.owned != nil {
		return e.writer.Write(p)
	}
	return w.WriteContext(e.ctx, p)
}

func (e *endpoint) read() *quickjs.Promise {
	return e.h.async(func() (any, error) {
		buf := make([]byte, 64<<10)
		n, err := e.readBytes(buf)
		if n > 0 {
			return buf[:n], nil
		}
		if errors.Is(err, io.EOF) {
			_ = e.Close()
			return nil, nil
		}
		return nil, err
	}, func(v any) (any, error) {
		if v == nil {
			return nil, nil
		}
		return e.h.rt.NewBytes(v.([]byte)), nil
	}, nil)
}

func (e *endpoint) write(chunk quickjs.Value) *quickjs.Promise {
	b, ok := chunk.Bytes()
	if !ok {
		p := e.h.rt.NewPromise()
		p.RejectError(errors.New("write requires Uint8Array"))
		return p
	}
	data := append([]byte(nil), b...)
	return e.h.async(func() (any, error) {
		n, err := e.writeBytes(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		return nil, err
	}, nil, nil)
}

func (e *endpoint) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.cancel()
	var err error
	if e.owned != nil {
		err = e.owned.Close()
	}
	if e.readAdapter != nil {
		err = errors.Join(err, e.readAdapter.Close())
	}
	if e.writeAdapter != nil {
		err = errors.Join(err, e.writeAdapter.Close())
	}
	e.mu.Unlock()
	e.h.release(e)
	return err
}
