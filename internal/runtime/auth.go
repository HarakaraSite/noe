package runtime

import (
	"encoding/json"
	"errors"
	"path/filepath"

	"noe/internal/credentials"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func (h *host) installAuth(opts Options) error {
	settings := credentials.Options{}
	if opts.Auth != nil {
		settings = *opts.Auth
	}
	return h.rt.SetModule("noe:auth/internal", map[string]any{
		"save": func(payload string) *quickjs.Promise {
			promise := h.rt.NewPromise()
			var request credentials.SaveRequest
			if err := json.Unmarshal([]byte(payload), &request); err != nil || request.File == "" {
				promise.RejectError(errors.New("invalid ChatGPT sign-in result"))
				return promise
			}
			if !filepath.IsAbs(request.File) {
				request.File = filepath.Join(h.cwd, request.File)
			}
			ctx := h.agentOperation.Context()
			h.loop.std.Begin()
			go func() {
				defer h.loop.std.Done()
				account, err := credentials.SaveChatGPT(ctx, request, settings)
				data, _ := json.Marshal(account)
				h.loop.std.Post(func() {
					if err != nil {
						promise.RejectError(err)
					} else {
						promise.Resolve(string(data))
					}
				})
			}()
			return promise
		},
	})
}
