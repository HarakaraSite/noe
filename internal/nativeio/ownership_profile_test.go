package nativeio

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	"forge.harakara.site/littleisland/noe/v2/internal/commandenv"
	"forge.harakara.site/littleisland/noe/v2/internal/process"
	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestHostCloseReclaimsSpawnBeforeJSDeliveryAndKeepsBorrowedVM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	rt := quickjs.New()
	defer rt.Close()
	loop := stdlib.NewLoop(rt)
	defer loop.Close()
	if err := stdlib.Install(rt, stdlib.Config{Loop: loop}); err != nil {
		t.Fatal(err)
	}
	host, err := InstallSelected(rt, Options{Command: &commandenv.Config{Allow: func(string, []string) error { return nil }, Timeout: time.Minute}}, Modules{Commands: true})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	payload, _ := json.Marshal(spawnRequest{Command: "/bin/sh", Args: []string{"-c", "while :; do :; done"}, Stdin: "ignore", Stdout: "ignore", Stderr: "ignore"})
	_ = host.spawn(string(payload))
	var child *process.Child
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		host.mu.Lock()
		for resource := range host.resources {
			if pending, ok := resource.(*process.Child); ok {
				child = pending
				break
			}
		}
		host.mu.Unlock()
		if child != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if child == nil {
		t.Fatal("spawn was not owned before its JS delivery")
	}
	host.Close()
	select {
	case <-child.Done:
	default:
		t.Fatal("Close returned before the undelivered child exited")
	}
	if loop.Context().Err() != nil {
		t.Fatal("borrowed loop was closed")
	}
	value, err := rt.Eval(`21*2`)
	if err != nil || value.Int() != 42 {
		t.Fatalf("borrowed VM=%v %v", value, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("late native delivery blocked borrowed loop: %v", err)
	}
}
