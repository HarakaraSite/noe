// This example keeps a Go HTTP host running, not a shared JavaScript VM.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

var sumSource = stdlib.Source{
	Name: "[sum]", Virtual: true, Module: true,
	Code: `const values = JSON.parse(process.env.INPUT_JSON);
const total = values.reduce((sum, value) => sum + value, 0);
if (!Number.isFinite(total)) throw new Error("total is not finite");
console.log(JSON.stringify({count: values.length, total}));`,
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	timeout := flag.Duration("request-timeout", 5*time.Second, "JavaScript execution timeout")
	flag.Parse()
	if *timeout <= 0 {
		log.Fatal("request-timeout must be positive")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *addr, *timeout); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, addr string, timeout time.Duration) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("listening on http://%s", listener.Addr())
	return serve(ctx, listener, newHandler(cwd, timeout, sumSource))
}

// The parent context also reaches active handlers through BaseContext.
// Shutdown stops accepting requests and waits for Execute (including VM close).
func serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	defer func() {
		_ = server.Close() // Best-effort cleanup on both normal and failed startup paths.
	}()

	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		log.Print("stopping: cancelling JavaScript executions and closing HTTP server")
	}

	// Do not derive this from ctx: the shutdown wait needs its own live deadline.
	shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return errors.Join(fmt.Errorf("HTTP shutdown: %w", err), server.Close())
	}
	if err := <-done; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Print("stopped")
	return nil
}

type executionResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// source is trusted Go-owned code, never JavaScript supplied by an HTTP client.
func newHandler(cwd string, timeout time.Duration, source stdlib.Source) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("POST /sum", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Values []*float64 `json:"values"`
		}
		// A small request limit keeps this demonstration's input bounded.
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&input); err != nil || input.Values == nil {
			http.Error(w, "expected {\"values\":[number,...]}", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			http.Error(w, "expected one JSON object", http.StatusBadRequest)
			return
		}
		for _, value := range input.Values {
			if value == nil {
				http.Error(w, "values must contain only numbers, not null", http.StatusBadRequest)
				return
			}
		}
		values, err := json.Marshal(input.Values)
		if err != nil {
			http.Error(w, "could not encode values", http.StatusInternalServerError)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		// Buffers, environment, VM and module cache all belong to this request.
		var stdout, stderr bytes.Buffer
		profile := stdlib.Profile{
			Environment: stdlib.Environment{
				Cwd: cwd, Args: []string{"embedding-server"},
				Env:    map[string]string{"INPUT_JSON": string(values)},
				Stdout: &stdout, Stderr: &stderr,
			},
			Modules: stdlib.Modules{ProcessInfo: true},
		}
		result, err := stdlib.Execute(ctx, source, profile)
		if err != nil {
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				http.Error(w, "JavaScript execution timed out", http.StatusGatewayTimeout)
			case errors.Is(err, context.Canceled):
				http.Error(w, "JavaScript execution cancelled", http.StatusServiceUnavailable)
			default:
				log.Printf("JavaScript: %s", stdlib.Diagnostic(err))
				http.Error(w, "JavaScript execution failed", http.StatusInternalServerError)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if result.Code != 0 {
			w.WriteHeader(http.StatusInternalServerError)
		}
		if err := json.NewEncoder(w).Encode(executionResponse{
			Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: result.Code,
		}); err != nil {
			log.Printf("write response: %v", err)
		}
	})
	return mux
}
