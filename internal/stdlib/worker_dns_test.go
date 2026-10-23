package stdlib

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
)

type loopDNSResolver struct{}

func (loopDNSResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("192.0.2.9")}, nil
}
func (loopDNSResolver) LookupAddr(context.Context, string) ([]string, error) {
	return nil, nil
}
func (loopDNSResolver) LookupCNAME(context.Context, string) (string, error) { return "", nil }
func (loopDNSResolver) LookupMX(context.Context, string) ([]*net.MX, error) { return nil, nil }
func (loopDNSResolver) LookupNS(context.Context, string) ([]*net.NS, error) { return nil, nil }
func (loopDNSResolver) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	return "", nil, nil
}
func (loopDNSResolver) LookupTXT(context.Context, string) ([]string, error) { return nil, nil }

func TestBindLoopCopiesDNSAndDeliversOnChildLoop(t *testing.T) {
	parentRT := quickjs.New()
	defer parentRT.Close()
	parentLoop := NewLoop(parentRT)
	parentRT.OnClose(parentLoop.Close)

	childRT := quickjs.New()
	defer childRT.Close()
	childLoop := NewLoop(childRT)
	childRT.OnClose(childLoop.Close)

	parentDNS := &DNS{Loop: parentLoop, Resolver: loopDNSResolver{}, Servers: []string{"192.0.2.53"}}
	cfg := Config{DNS: parentDNS}
	cfg.bindLoop(childLoop)
	if cfg.DNS == parentDNS || cfg.DNS.Loop != childLoop {
		t.Fatal("worker DNS was not copied and rebound to its loop")
	}
	if parentDNS.Loop != parentLoop || parentDNS.Servers[0] != "192.0.2.53" {
		t.Fatal("binding the worker changed parent DNS settings")
	}
	cfg.DNS.Servers[0] = "198.51.100.53"
	if parentDNS.Servers[0] != "192.0.2.53" {
		t.Fatal("worker DNS shares the parent's Servers slice")
	}

	if err := Names(childRT, cfg.DNS); err != nil {
		t.Fatal(err)
	}
	if _, err := childRT.Eval(`
		import("dns/promises").then(({default: dns}) =>
			dns.lookup("worker.example.test").then(answer => globalThis.answer = answer.address));
	`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := childLoop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	answer, err := childRT.Eval("answer")
	if err != nil || answer.String() != "192.0.2.9" {
		t.Fatalf("child DNS answer = %q, %v", answer.String(), err)
	}
	if parentLoop.Pending() {
		t.Fatal("DNS notification was posted to the parent loop")
	}
}
