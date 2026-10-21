package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestSumRequests(t *testing.T) {
	server := httptest.NewServer(newHandler(t.TempDir(), time.Second, sumSource))
	defer server.Close()

	// Concurrent requests must not share their output buffers or input environment.
	var wg sync.WaitGroup
	for _, input := range []string{
		`{"values":[7,11,13]}`, `{"values":[]}`, `{"values":[-2,0.5]}`,
	} {
		wg.Go(func() {
			resp, err := server.Client().Post(server.URL+"/sum", "application/json", strings.NewReader(input))
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			var output executionResponse
			if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
				t.Error(err)
				return
			}
			var request struct{ Values []float64 }
			if err := json.Unmarshal([]byte(input), &request); err != nil {
				t.Error(err)
				return
			}
			var want float64
			for _, value := range request.Values {
				want += value
			}
			var got struct {
				Count int
				Total float64
			}
			if err := json.Unmarshal([]byte(output.Stdout), &got); err != nil {
				t.Error(err)
				return
			}
			if resp.StatusCode != http.StatusOK || output.ExitCode != 0 || output.Stderr != "" || got.Total != want || got.Count != len(request.Values) {
				t.Errorf("input=%s status=%d output=%+v sum=%+v", input, resp.StatusCode, output, got)
			}
		})
	}
	wg.Wait()
}

func TestFreshRuntimeAndOutput(t *testing.T) {
	source := stdlib.Source{Name: "[isolation]", Virtual: true, Module: true, Code: `
		globalThis.calls = (globalThis.calls || 0) + 1;
		console.log(globalThis.calls);
		console.error(process.env.INPUT_JSON);
	`}
	handler := newHandler(t.TempDir(), time.Second, source)
	for range 2 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("POST", "/sum", strings.NewReader(`{"values":[42]}`)))
		var output executionResponse
		if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || output.Stdout != "1\n" || output.Stderr != "[42]\n" {
			t.Fatalf("status=%d output=%+v", response.Code, output)
		}
	}
}

func TestExecutionStatus(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		status     int
	}{
		{"exception", `throw new Error("example failure")`, http.StatusInternalServerError},
		{"exit-code", `process.exitCode = 7`, http.StatusInternalServerError},
		{"cpu-deadline", `while (true) {}`, http.StatusGatewayTimeout},
		{"async-deadline", `await new Promise(resolve => setTimeout(resolve, 60000))`, http.StatusGatewayTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			timeout := time.Second
			if tc.status == http.StatusGatewayTimeout {
				timeout = 25 * time.Millisecond
			}
			source := stdlib.Source{Name: "[status]", Virtual: true, Module: true, Code: tc.code}
			response := httptest.NewRecorder()
			newHandler(t.TempDir(), timeout, source).ServeHTTP(response, httptest.NewRequest("POST", "/sum", strings.NewReader(`{"values":[]}`)))
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			if tc.name == "exit-code" {
				var output executionResponse
				if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
					t.Fatal(err)
				}
				if output.ExitCode != 7 {
					t.Fatalf("exitCode=%d", output.ExitCode)
				}
			}
		})
	}
}

func TestCancelledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest("POST", "/sum", strings.NewReader(`{"values":[]}`)).WithContext(ctx)
	response := httptest.NewRecorder()
	newHandler(t.TempDir(), time.Second, sumSource).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestInvalidInput(t *testing.T) {
	handler := newHandler(t.TempDir(), time.Second, sumSource)
	for _, body := range []string{`{}`, `{"values":["x"]}`, `{"values":[1,null,2]}`, `{"values":[null]}`, `{"values":[]} {}`} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("POST", "/sum", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d", body, response.Code)
		}
	}
}

func TestShutdownCancelsExecutionAndWaitsForCleanup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, cleaned := make(chan struct{}), make(chan struct{})
	executionDone := make(chan error, 1)
	cwd := t.TempDir()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		profile := stdlib.Profile{
			Environment: stdlib.Environment{Cwd: cwd},
			Initialize: func(context.Context, *stdlib.Bindings) (func(), error) {
				close(started)
				return func() { close(cleaned) }, nil
			},
		}
		_, err := stdlib.Execute(r.Context(), stdlib.Source{
			Name: "[shutdown]", Virtual: true, Module: true,
			Code: `await new Promise(resolve => setTimeout(resolve, 60000))`,
		}, profile)
		executionDone <- err
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	serverDone := make(chan error, 1)
	go func() { serverDone <- serve(ctx, listener, handler) }()
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			err = resp.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not start")
	}
	cancel()
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("server returned before runtime cleanup")
	}
	if err := <-executionDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("execution error=%v", err)
	}
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err == nil {
		connection.Close()
		t.Fatal("listener still accepting connections")
	}
}
