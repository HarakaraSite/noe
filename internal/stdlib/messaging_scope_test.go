package stdlib

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// TestMessageChannelScopeStopsQueuedDelivery releases a listening port and
// suppresses its already queued task when its borrowed execution ends.
func TestMessageChannelScopeStopsQueuedDelivery(t *testing.T) {
	senderRT := quickjs.New()
	sender, err := webAPIs(senderRT, rand.Reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		sender.shutdown(nil)
		senderRT.Close()
	}()

	targetRT := quickjs.New()
	targetLoop := NewLoop(targetRT)
	scope, err := NewExecutionScope(targetLoop, context.Background())
	if err != nil {
		targetRT.Close()
		t.Fatal(err)
	}
	target, err := webAPIs(targetRT, rand.Reader, targetLoop)
	if err != nil {
		scope.Close()
		targetLoop.Close()
		targetRT.Close()
		t.Fatal(err)
	}
	defer func() {
		scope.Close()
		targetLoop.Close()
		targetRT.Close()
	}()

	channel, err := senderRT.Eval(`new MessageChannel()`)
	if err != nil {
		t.Fatal(err)
	}
	senderPort, err := channel.Get("port1")
	if err != nil {
		t.Fatal(err)
	}
	peerPort, err := channel.Get("port2")
	if err != nil {
		t.Fatal(err)
	}
	serialized, transferred, err := sender.serialize(senderPort, []quickjs.Value{senderPort}, nil)
	if err != nil {
		t.Fatal(err)
	}
	targetPort, _, err := target.deserialize(serialized, transferred)
	if err != nil {
		t.Fatal(err)
	}
	if err := targetRT.Set("port1", targetPort); err != nil {
		t.Fatal(err)
	}
	if err := senderRT.Set("port2", peerPort); err != nil {
		t.Fatal(err)
	}
	if _, err := targetRT.Eval(`
		globalThis.scopeMessageCalls = 0;
		port1.onmessage = () => { scopeMessageCalls++ };
	`); err != nil {
		t.Fatal(err)
	}
	if !targetLoop.Pending() {
		t.Fatal("the adopted listening MessagePort did not hold its loop")
	}
	// The message is sent from another Runtime, so its host task is queued on
	// targetRT without driving that borrowed runtime yet.
	if _, err := senderRT.Eval(`port2.postMessage("queued before scope close")`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-targetRT.Wake():
	default:
		t.Fatal("the target runtime did not receive the queued MessagePort task")
	}

	scope.Close()
	if targetLoop.Pending() {
		t.Fatal("scope cleanup left the MessagePort's loop hold outstanding")
	}
	if len(target.ports) != 0 {
		t.Fatalf("scope cleanup left %d owned MessagePorts", len(target.ports))
	}
	if targetLoop.Context().Err() != nil || targetRT.Context().Err() != nil {
		t.Fatal("scope cleanup closed its borrowed Loop or Runtime")
	}

	callerPostRan := false
	targetLoop.Post(func() { callerPostRan = true })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := targetLoop.Run(ctx); err != nil {
		t.Fatalf("Loop.Run after scope cleanup = %v", err)
	}
	if !callerPostRan {
		t.Fatal("scope cleanup suppressed caller-owned Loop.Post work")
	}
	calls, err := targetRT.Get("scopeMessageCalls")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Int() != 0 {
		t.Fatalf("a MessagePort listener ran %d times after its scope closed", calls.Int())
	}
}
