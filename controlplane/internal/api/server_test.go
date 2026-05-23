package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dlh/dlh-test-fw/controlplane/internal/auth"
	"github.com/dlh/dlh-test-fw/controlplane/internal/runs"
)

// alwaysRejectMW is a bearer-auth middleware stub that rejects every request
// so we can verify which paths are exempt from auth.
func alwaysRejectMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rejected by test middleware", http.StatusUnauthorized)
	})
}

func TestNewRouter_AuthInfoExemptFromBearer(t *testing.T) {
	deps := &Deps{
		Manifests: &runs.ManifestWriter{},
		AuthInfo:  AuthInfoConfig{AuthDisabled: true},
	}
	handler := NewRouter(deps, alwaysRejectMW, "tok")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/info", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Errorf("/api/auth/info must be accessible without auth (login bootstrap), got 401")
	}
}

func TestNewRouter_OtherAPIPaths_RequireBearer(t *testing.T) {
	authMW := auth.Middleware(auth.FakeVerifier{}, &auth.Roles{}, nil)

	deps := &Deps{
		Manifests: &runs.ManifestWriter{},
		AuthInfo:  AuthInfoConfig{AuthDisabled: false},
	}
	handler := NewRouter(deps, authMW, "tok")

	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	// No Authorization header — should get 401.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("/api/runs without Bearer token: want 401, got %d", w.Code)
	}
}
