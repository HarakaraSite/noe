package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLowLevelFileREPLRetainsHandleWithoutKeepalive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.bin"), []byte{10, 20, 30}, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(Options{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, input := range []string{
		`const fs=await import("noe:fs-handle")`,
		`const file=await fs.open("data.bin",{mode:"r+"})`,
		`await file.writeAt(1,new Uint8Array([99]))`,
		`await file.readAt(0,4).then(r=>{if(r.bytesRead!==3||!r.eof||r.data[1]!==99)throw Error("retained handle");return true})`,
		`await file.close()`,
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		result := s.Eval(ctx, input)
		cancel()
		if result.Err != nil || result.Exit {
			t.Fatalf("input=%s result=%+v", input, result)
		}
	}
}

func TestLowLevelREPLCancellationReclaimsOwnedResources(t *testing.T) {
	for _, source := range []string{
		`const {listen}=await import("noe:sockets");await listen({network:"tcp",host:"127.0.0.1",port:0});`,
		`const {subscribe}=await import("noe:signals");subscribe("SIGINT");`,
	} {
		t.Run(source, func(t *testing.T) {
			s, err := NewSession(Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			result := s.Eval(ctx, `await (async()=>{`+source+`})()`)
			if result.Err == nil || !result.Exit || s.h.ioHost.Active() {
				t.Fatalf("interrupted resource remained: result=%+v active=%v", result, s.h.ioHost.Active())
			}
		})
	}
}
