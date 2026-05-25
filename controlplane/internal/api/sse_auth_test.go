package api

import (
	"net/http/httptest"
	"testing"
)

func TestBearerOrQueryToken(t *testing.T) {
	r1 := httptest.NewRequest("GET", "/api/runs/x/events", nil)
	r1.Header.Set("Authorization", "Bearer hdr-tok")
	if got := bearerOrQueryToken(r1); got != "hdr-tok" {
		t.Fatalf("header token: got %q", got)
	}
	r2 := httptest.NewRequest("GET", "/api/runs/x/events?access_token=q-tok", nil)
	if got := bearerOrQueryToken(r2); got != "q-tok" {
		t.Fatalf("query token: got %q", got)
	}
	r3 := httptest.NewRequest("GET", "/api/runs/x/events", nil)
	if got := bearerOrQueryToken(r3); got != "" {
		t.Fatalf("no token: got %q", got)
	}
}
