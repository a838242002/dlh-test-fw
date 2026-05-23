package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

// ctxKey scopes context keys so they don't collide with other packages.
type ctxKey int

const (
	identityKey ctxKey = iota
	roleKey
)

// Middleware verifies bearer tokens and attaches the identity + role to
// the request context. It accepts controlplane session JWTs (verified locally
// via HMAC) or OIDC bearer tokens (verified against remote issuer).
func Middleware(v VerifierIface, roles *Roles, sessionIssuer *SessionIssuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHdr := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHdr, "Bearer ") {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(authHdr, "Bearer ")

			var id *Identity
			var err error
			// Try controlplane session JWT first (cheap, local HMAC verify).
			if sessionIssuer != nil {
				id, err = sessionIssuer.Verify(token)
			}
			// Fall back to OIDC bearer verification.
			if id == nil {
				id, err = v.Verify(r.Context(), token)
			}
			if err != nil || id == nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			role := roles.Resolve(id)
			ctx := context.WithValue(r.Context(), identityKey, id)
			ctx = context.WithValue(ctx, roleKey, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole returns a middleware that enforces a minimum role.
func RequireRole(want Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := r.Context().Value(roleKey).(Role)
			if !role.IsAtLeast(want) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IdentityFromContext extracts the verified identity, if present.
func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(identityKey).(*Identity)
	return id, ok
}

// InternalTokenMiddleware verifies X-Internal-Token matches the configured
// shared secret. Used for /internal/* endpoints called by Workflow http steps.
// ConstantTimeCompare avoids token-length side-channels.
func InternalTokenMiddleware(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("X-Internal-Token")
			if expected == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
				http.Error(w, "bad internal token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
