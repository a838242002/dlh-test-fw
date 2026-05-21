package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Verifier wraps oidc.IDTokenVerifier with an audience check and groups
// claim extraction.
type Verifier struct {
	v           *oidc.IDTokenVerifier
	groupsClaim string
	requiredAud string
}

// NewVerifier builds a Verifier from the issuer URL + client ID + optional
// audience (defaults to client ID if empty).
func NewVerifier(ctx context.Context, issuer, clientID, audience, groupsClaim string) (*Verifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider: %w", err)
	}
	aud := audience
	if aud == "" {
		aud = clientID
	}
	if groupsClaim == "" {
		groupsClaim = "groups"
	}
	return &Verifier{
		v:           provider.Verifier(&oidc.Config{ClientID: clientID}),
		groupsClaim: groupsClaim,
		requiredAud: aud,
	}, nil
}

// Identity is the subset of token claims we care about.
type Identity struct {
	Subject string
	Email   string
	Groups  []string
}

// Verify validates the bearer token and returns the identity.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Identity, error) {
	if v == nil {
		return nil, errors.New("verifier not configured")
	}
	tok, err := v.v.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	var stdClaims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if err := tok.Claims(&stdClaims); err != nil {
		return nil, fmt.Errorf("claims: %w", err)
	}
	id := &Identity{Subject: stdClaims.Sub, Email: stdClaims.Email}
	// Re-decode raw claims to extract groups under the configured key.
	rawClaims := map[string]any{}
	_ = tok.Claims(&rawClaims)
	if groups, ok := rawClaims[v.groupsClaim].([]any); ok {
		for _, g := range groups {
			if s, ok := g.(string); ok {
				id.Groups = append(id.Groups, s)
			}
		}
	}
	return id, nil
}
