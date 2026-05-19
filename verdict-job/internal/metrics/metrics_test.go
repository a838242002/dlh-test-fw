package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
)

func TestPushSerializesAndPOSTs(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/import/prometheus" {
			t.Errorf("path = %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	r := &eval.Result{
		Overall: false,
		Thresholds: []eval.ThresholdResult{
			{Metric: "http_5xx_rate", Value: 0, Passed: true},
			{Metric: "p95_latency_ms", Value: 12.5, Passed: false},
		},
		ChaosWindowStart: time.Unix(1779210000, 0),
		ChaosWindowEnd:   time.Unix(1779210060, 0),
	}
	p := New(srv.URL + "/api/v1/import/prometheus")
	if err := p.Push(context.Background(), "wf-123", "mysql-pod-delete", r); err != nil {
		t.Fatalf("push: %v", err)
	}

	wantLines := []string{
		`dlh_verdict_overall{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete"} 0`,
		`dlh_verdict_threshold_pass{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete",name="http_5xx_rate"} 1`,
		`dlh_verdict_threshold_value{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete",name="http_5xx_rate"} 0`,
		`dlh_verdict_threshold_pass{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete",name="p95_latency_ms"} 0`,
		`dlh_verdict_threshold_value{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete",name="p95_latency_ms"} 12.5`,
		`dlh_chaos_window_start_unixtime{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete"} 1779210000`,
		`dlh_chaos_window_end_unixtime{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete"} 1779210060`,
	}
	for _, line := range wantLines {
		if !strings.Contains(got, line) {
			t.Errorf("missing line in payload:\n  want: %s\n  got:\n%s", line, got)
		}
	}
}

func TestPushNon2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	p := New(srv.URL)
	err := p.Push(context.Background(), "w", "s", &eval.Result{})
	if err == nil {
		t.Fatalf("expected error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should include status: %v", err)
	}
}
