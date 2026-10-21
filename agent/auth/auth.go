// Package auth verifies and saves helper-owned sign-in results. It does not
// create an agent Host, discover credential locations, or own its HTTP client.
package auth

import (
	"context"

	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
)

type HTTPClient = credentials.HTTPClient
type Options = credentials.Options
type Tokens = credentials.Tokens
type SaveRequest = credentials.SaveRequest
type Account = credentials.Account

// PlanScope authorizes ChatGPT plan usage rather than API-key billing.
const PlanScope = credentials.PlanScope

// SaveChatGPT validates the supplied sign-in result and atomically saves it at
// request.File. The caller explicitly selects that file; no NOE paths or
// environment variables are consulted. Refresh uses this same record later.
func SaveChatGPT(ctx context.Context, request SaveRequest, options Options) (Account, error) {
	return credentials.SaveChatGPT(ctx, request, options)
}
