package agentbridge

import (
	"context"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestManagedBindingClosePreservesBorrowedRuntimeAndLoop(t *testing.T) {
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	t.Cleanup(func() { _ = rt.Close() })

	view, err := stdlib.Attach(rt, loop, stdlib.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	host := core.NewHost(core.Options{})
	defer host.Close()
	op := host.NewOperation(context.Background())
	binding, err := AttachOperation(context.Background(), view, host, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttachOperation(context.Background(), view, host, host.NewOperation(context.Background())); err == nil {
		t.Fatal("a runtime accepted a second managed binding")
	}

	binding.Close()
	if op.Context().Err() == nil {
		t.Fatal("binding Close left its owned operation alive")
	}
	if rt.Context().Err() != nil {
		t.Fatal("binding Close canceled the caller's runtime")
	}
	if loop.Context().Err() != nil {
		t.Fatal("binding Close canceled the caller's loop")
	}
	value, err := rt.Eval(`1 + 1`)
	if err != nil || value.Int() != 2 {
		t.Fatalf("borrowed runtime did not remain usable: value=%v err=%v", value, err)
	}
}

func TestManagedOperationBindingWaitsForOwnerAfterViewCancellation(t *testing.T) {
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	t.Cleanup(func() { _ = rt.Close() })

	view, err := stdlib.Attach(rt, loop, stdlib.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	host := core.NewHost(core.Options{})
	defer host.Close()
	op := host.NewOperation(context.Background())
	binding, err := AttachOperation(context.Background(), view, host, op)
	if err != nil {
		t.Fatal(err)
	}

	// A managed owner closes its binding before closing the loop. If a view
	// cancellation arrives first (as it does for process.exit), the binding
	// must leave the operation alive until that owner joins it.
	loop.Close()
	if view.Context().Err() == nil {
		t.Fatal("closing the loop did not cancel the borrowed view context")
	}
	if op.Context().Err() != nil {
		t.Fatal("view cancellation closed the owned operation before its owner")
	}
	if binding.isClosed() {
		t.Fatal("view cancellation closed a managed binding before its owner")
	}

	binding.Close()
	if op.Context().Err() == nil {
		t.Fatal("owner Close left its operation alive")
	}
	if rt.Context().Err() != nil {
		t.Fatal("managed binding Close canceled the borrowed runtime")
	}
}

func TestClientBindingStillClosesOnContextCancellation(t *testing.T) {
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	t.Cleanup(func() { _ = rt.Close() })
	view, err := stdlib.Attach(rt, loop, stdlib.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	host := core.NewHost(core.Options{})
	defer host.Close()
	ctx, cancel := context.WithCancel(context.Background())
	op := host.NewOperation(context.Background())
	binding, err := AttachClient(ctx, view, host, op)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-binding.closeDone:
	case <-time.After(time.Second):
		t.Fatal("client binding did not close after its context was cancelled")
	}
	if !binding.isClosed() || op.Context().Err() == nil {
		t.Fatal("client context cancellation did not close its owned operation")
	}
	if rt.Context().Err() != nil || loop.Context().Err() != nil {
		t.Fatal("client binding cancellation closed the borrowed runtime or loop")
	}
}
