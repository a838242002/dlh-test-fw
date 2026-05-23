package api

import (
	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
)

func (h *Handlers) handleAuthInfo() gen.GetAuthInfoResponseObject {
	cfg := h.deps.AuthInfo
	ciAud := cfg.CIAudience
	disabled := cfg.AuthDisabled
	return gen.GetAuthInfo200JSONResponse{
		OidcIssuer:   cfg.OIDCIssuer,
		OidcClientId: cfg.OIDCClientID,
		CiAudience:   &ciAud,
		AuthDisabled: &disabled,
	}
}
