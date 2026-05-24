package api

import (
	"net/http"
	"strings"
)

// bearerOrQueryToken returns the request's auth token from the Authorization
// header ("Bearer <t>") or, failing that, the ?access_token= query param.
// EventSource cannot set headers, so SSE clients pass the token by query.
func bearerOrQueryToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("access_token")
}
