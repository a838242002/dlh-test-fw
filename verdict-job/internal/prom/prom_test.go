package prom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQueryAtParsesScalarLikeResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if q := r.URL.Query().Get("query"); q != "up" {
			t.Errorf("unexpected query %q", q)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result": []map[string]any{{
					"metric": map[string]string{},
					"value":  []any{1700000000.0, "42.5"},
				}},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	v, err := c.QueryAt(context.Background(), "up", time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if v != 42.5 {
		t.Errorf("got %v want 42.5", v)
	}
}

func TestQueryAtEmptyResultReturnsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "vector", "result": []any{}},
		})
	}))
	defer srv.Close()
	v, err := New(srv.URL).QueryAt(context.Background(), "up", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Errorf("empty result: got %v want 0", v)
	}
}
