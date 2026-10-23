package stdlib

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
)

const serveReadSize = 32 << 10

// Match the ASCII token boundaries used by Go's request.expectsContinue.
// Header.Get intentionally reads the same first field value that Go uses.
func serveExpectsContinue(header http.Header) bool {
	for _, token := range strings.FieldsFunc(header.Get("Expect"), func(r rune) bool {
		return r == ' ' || r == '\t' || r == ','
	}) {
		if strings.EqualFold(token, "100-continue") {
			return true
		}
	}
	return false
}

// serveBody owns only the input half of an exchange. Logical cancellation
// stops demand immediately; interrupting the connection read can wait until
// the response has been flushed, so it cannot discard that response.
type serveBody struct {
	mu        sync.Mutex
	body      io.ReadCloser
	remaining int64
	nextErr   error
	eof       bool
	canceled  bool
	closed    bool
	reading   bool
	localStop bool
	reads     sync.WaitGroup
}

func (b *serveBody) cancel() {
	b.mu.Lock()
	b.canceled = true
	b.mu.Unlock()
}

func (b *serveBody) complete() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.eof
}

func (b *serveBody) stopped() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed || b.canceled
}

func (b *serveBody) locallyStopped() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.localStop
}

// interruptCanceled is needed before the first Expect response flush: Go
// takes the body read lock even with full duplex enabled. Mark the reason
// before setting the deadline, since that deadline also cancels r.Context.
func (b *serveBody) interruptCanceled(rc *http.ResponseController) {
	b.mu.Lock()
	interrupt := b.canceled && !b.eof
	if interrupt {
		b.localStop = true
	}
	b.mu.Unlock()
	if interrupt {
		rc.SetReadDeadline(time.Now())
		b.reads.Wait()
	}
}

func (b *serveBody) close(rc *http.ResponseController) {
	b.mu.Lock()
	b.closed = true
	interrupt := !b.eof
	if interrupt {
		b.localStop = true
	}
	b.mu.Unlock()
	if interrupt {
		rc.SetReadDeadline(time.Now())
	}
	b.reads.Wait()
	b.body.Close()
}

func (b *serveBody) read() ([]byte, error) {
	b.mu.Lock()
	if b.closed || b.canceled {
		b.mu.Unlock()
		return nil, errors.New("the request body is closed")
	}
	if b.nextErr != nil {
		err := b.nextErr
		b.mu.Unlock()
		return nil, err
	}
	if b.remaining <= 0 || b.eof {
		b.mu.Unlock()
		return nil, io.EOF
	}
	if b.reading {
		b.mu.Unlock()
		return nil, errors.New("the request body already has a pending read")
	}
	n := int64(serveReadSize)
	if b.remaining < n {
		n = b.remaining
	}
	b.reading = true
	b.reads.Add(1)
	b.mu.Unlock()
	defer b.reads.Done()
	data := make([]byte, int(n))
	nread, err := b.body.Read(data)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reading = false
	if b.closed || b.canceled {
		return nil, errors.New("the request body is closed")
	}
	b.remaining -= int64(nread)
	if err == io.EOF {
		b.eof = true
	}
	if nread > 0 {
		b.nextErr = err
		return data[:nread], nil
	}
	return data[:0], err
}

func (b *serveBody) reader(rt *quickjs.Runtime, loop *Loop) func() *quickjs.Promise {
	return func() *quickjs.Promise {
		p := rt.NewPromise()
		loop.Begin()
		if err := loop.Go(func(context.Context) {
			defer loop.Done()
			data, err := b.read()
			loop.postExecution(func() {
				switch {
				case b.stopped():
					p.RejectError(errors.New("the request body is closed"))
				case err == io.EOF:
					p.Resolve(nil)
				case err != nil:
					p.RejectError(err)
				default:
					p.Resolve(rt.NewBytes(data))
				}
			})
		}); err != nil {
			loop.Done()
			p.RejectError(err)
		}
		return p
	}
}

// serveRequest does all connection IO on the HTTP goroutine. Nothing queued
// to the JS owner is required to release its reads or finish this handler.
func (s *servers) serveRequest(w http.ResponseWriter, r *http.Request, dispatch quickjs.Value, limit int64) {
	loop := s.cfg.Loop
	rc := http.NewResponseController(w)
	// HTTP/1 normally drains unread input before writing a response.
	rc.EnableFullDuplex()
	ex := &exchange{
		method: r.Method, url: requestURL(r),
		input:   &serveBody{body: r.Body, remaining: limit, eof: r.Body == http.NoBody || r.Body == nil},
		hasBody: r.Method != "GET" && r.Method != "HEAD" && r.Body != nil && r.Body != http.NoBody,
		expect:  serveExpectsContinue(r.Header),
		answer:  make(chan *reply, 1), chunks: make(chan []byte),
		gone: make(chan struct{}), ready: make(chan struct{}),
	}
	hijacked := false
	defer func() {
		close(ex.gone)
		if !hijacked {
			ex.input.close(rc)
		}
	}()
	if ex.expect {
		// An explicit informational response leaves future Body.Read enabled,
		// even if final headers are sent before JS first asks for input.
		w.WriteHeader(http.StatusContinue)
	}
	names := make([]string, 0, len(r.Header))
	for name := range r.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, value := range r.Header[name] {
			ex.headers = append(ex.headers, [2]string{name, value})
		}
	}
	loop.postExecution(func() {
		select {
		case <-ex.gone:
			return
		default:
			s.handle(dispatch, ex)
		}
	})
	var res *reply
	select {
	case res = <-ex.answer:
	case <-r.Context().Done():
		return
	case <-loopContext(loop).Done():
		return
	}
	if res.upgrade != "" {
		// Successful hijack transfers ownership to the existing socket path.
		hijacked = s.takeOver(w, r, res)
		return
	}
	if ex.expect {
		ex.input.interruptCanceled(rc)
	}
	if res.err != nil {
		res = &reply{status: http.StatusInternalServerError, body: []byte("the handler failed\n")}
	}
	for _, h := range res.headers {
		w.Header().Add(h[0], h[1])
	}
	if !ex.input.complete() {
		w.Header().Set("Connection", "close")
	}
	status := res.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if !res.streaming {
		if _, err := w.Write(res.body); err == nil {
			rc.Flush()
		}
		return
	}
	if ex.expect {
		if err := rc.Flush(); err != nil {
			return
		}
	}
	close(ex.ready)
	requestDone := r.Context().Done()
	for {
		select {
		case chunk, ok := <-ex.chunks:
			if !ok {
				if ex.failed {
					panic(http.ErrAbortHandler)
				}
				rc.Flush()
				return
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case <-requestDone:
			if !ex.input.locallyStopped() {
				return
			}
			// An intentional Expect input interruption can cancel Go's request
			// context. Keep sending; real write failures and runtime closure
			// still terminate the output half.
			requestDone = nil
		case <-loopContext(loop).Done():
			return
		}
	}
}

// serverLifetime closes admission before waiting so handler Add cannot race
// a zero WaitGroup. A forced close is independent of graceful Shutdown.
type serverLifetime struct {
	mu        sync.Mutex
	closing   bool
	handlers  sync.WaitGroup
	forceOnce sync.Once
	doneOnce  sync.Once
	loop      *Loop
}

func newServerLifetime(loop *Loop) *serverLifetime { return &serverLifetime{loop: loop} }

func (l *serverLifetime) enter() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closing {
		return false
	}
	l.handlers.Add(1)
	return true
}

func (l *serverLifetime) leave() { l.handlers.Done() }

func (l *serverLifetime) stopAdmission() {
	l.mu.Lock()
	l.closing = true
	l.mu.Unlock()
}

func (l *serverLifetime) wait() {
	l.handlers.Wait()
	l.doneOnce.Do(l.loop.Done)
}

func (l *serverLifetime) force(srv *http.Server) {
	l.forceOnce.Do(func() {
		l.stopAdmission()
		srv.Close()
		l.wait()
	})
}
