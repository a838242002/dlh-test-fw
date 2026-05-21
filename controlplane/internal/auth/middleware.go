package auth

import (
	"context"
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
// the request context.
func Middleware(v VerifierIface, roles *Roles) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHdr := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHdr, "Bearer ") {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(authHdr, "Bearer ")
			id, err := v.Verify(r.Context(), token)
			if err != nil {
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
