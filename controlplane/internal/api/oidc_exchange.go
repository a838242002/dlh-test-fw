package api

import (
	"context"
	"time"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
)

func (h *Handlers) handleOidcExchange(ctx context.Context, body *gen.ExchangeRequest) (gen.OidcExchangeResponseObject, error) {
	if body == nil || body.Token == "" {
		return gen.OidcExchange400Response{}, nil
	}
	if h.deps.Exchanger == nil || h.deps.SessionIssuer == nil {
		return gen.OidcExchange401Response{}, nil
	}
	id, err := h.deps.Exchanger.Validate(ctx, body.Token)
	if err != nil {
		return gen.OidcExchange401Response{}, nil
	}
	tok, err := h.deps.SessionIssuer.Issue(id)
	if err != nil {
		return gen.OidcExchange401Response{}, nil
	}
	expires := nowPlusLifetime(h.deps.SessionIssuer.Lifetime)
	groups := append([]string(nil), id.Groups...)
	return gen.OidcExchange200JSONResponse{
		AccessToken: tok,
		ExpiresAt:   expires,
		Subject:     &id.Subject,
		Groups:      &groups,
	}, nil
}

func nowPlusLifetime(d time.Duration) time.Time {
	if d == 0 {
		d = time.Hour
	}
	return time.Now().UTC().Add(d)
}
