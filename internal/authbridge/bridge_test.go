package authbridge

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

type blockingHTTP struct {
	started chan struct{}
	stopped chan struct{}
}

func (c *blockingHTTP) Do(r *http.Request) (*http.Response, error) {
	close(c.started)
	<-r.Context().Done()
	close(c.stopped)
	return nil, r.Context().Err()
}

// A host-free runtime can install auth, and closing that installation cancels
// HTTP without waiting for the owner to deliver a Promise completion.
func TestIndependentBindingCloseJoinsHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	defer rt.Close()
	client := &blockingHTTP{make(chan struct{}), make(chan struct{})}
	b, err := Install(ctx, rt, loop, t.TempDir(), credentials.Options{HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	rt.SetModuleLoader(func(specifier, _ string) (string, string, error) {
		if specifier == "noe:auth" {
			return credentials.ModuleSource, specifier, nil
		}
		return "", "", errors.New("unexpected module")
	})
	_, err = rt.EvalContext(ctx, `import("noe:auth").then(({save_chatgpt_credentials: save}) => save({
file:"account.json",clientId:"client",hostId:"host",nonce:"nonce",
tokens:{access_token:"fixture",id_token:"fixture",expires_in:60,scope:"chatgpt.tokens.use.direct"}
})).catch(()=>{});`)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-client.started:
		case <-ctx.Done():
			return
		}
		var group sync.WaitGroup
		for range 3 {
			group.Go(b.Close)
		}
		group.Wait()
	}()
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	<-done
	select {
	case <-client.stopped:
	default:
		t.Fatal("Close did not join HTTP")
	}
	if rt.Context().Err() != nil || loop.Context().Err() != nil {
		t.Fatal("borrowed runtime or loop was closed")
	}
	if _, err := rt.Eval("1+1"); err != nil {
		t.Fatal(err)
	}
}
