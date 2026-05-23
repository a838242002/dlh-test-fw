package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Exchanger validates external OIDC tokens (GH Actions JWT, GitLab JWT, etc.)
// against a configured issuer allowlist + required audience. On success
// returns an Identity built from the token's standard claims.
type Exchanger struct {
	// TrustedIssuers is the set of acceptable `iss` values.
	TrustedIssuers []string
	// RequiredAudience is checked against the token's `aud` claim.
	RequiredAudience string
	// providers caches per-issuer go-oidc providers.
	providers map[string]*oidc.Provider
}

// ErrIssuerNotTrusted indicates the token's iss doesn't match the allowlist.
var ErrIssuerNotTrusted = errors.New("token issuer not in trusted allowlist")

// Validate parses the token (no verification yet) to extract iss, looks
// up the matching provider, and runs full verification. Returns an
// Identity on success.
func (e *Exchanger) Validate(ctx context.Context, rawToken string) (*Identity, error) {
	if rawToken == "" {
		return nil, errors.New("empty token")
	}
	// Quick parse to get iss before paying for verification.
	parsed, err := parseIssuerOnly(rawToken)
	if err != nil {
		return nil, fmt.Errorf("parse iss: %w", err)
	}
	if !contains(e.TrustedIssuers, parsed) {
		return nil, fmt.Errorf("%w: %s", ErrIssuerNotTrusted, parsed)
	}
	prov, err := e.providerFor(ctx, parsed)
	if err != nil {
		return nil, fmt.Errorf("oidc provider %s: %w", parsed, err)
	}
	verifier := prov.Verifier(&oidc.Config{
		ClientID: e.RequiredAudience,
	})
	tok, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	// CI claims are a free-for-all; build Identity from standard fields
	// plus repository / repository_owner that GH Actions sets.
	claims := map[string]any{}
	_ = tok.Claims(&claims)
	subject, _ := claims["sub"].(string)
	if subject == "" {
		subject = tok.Subject
	}
	email, _ := claims["email"].(string)
	id := &Identity{Subject: subject, Email: email}
	if repo, ok := claims["repository"].(string); ok && repo != "" {
		id.Groups = append(id.Groups, "ci-repo:"+repo)
	}
	if owner, ok := claims["repository_owner"].(string); ok && owner != "" {
		id.Groups = append(id.Groups, "ci-owner:"+owner)
	}
	return id, nil
}

func (e *Exchanger) providerFor(ctx context.Context, issuer string) (*oidc.Provider, error) {
	if e.providers == nil {
		e.providers = map[string]*oidc.Provider{}
	}
	if p, ok := e.providers[issuer]; ok {
		return p, nil
	}
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	e.providers[issuer] = p
	return p, nil
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
