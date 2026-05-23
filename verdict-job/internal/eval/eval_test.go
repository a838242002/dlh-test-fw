package eval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/prom"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/slo"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/window"
)

func ptr(f float64) *float64 { return &f }

func TestEvaluatePassAllGreen(t *testing.T) {
	s := &slo.SLO{
		Thresholds: []slo.Threshold{
			{Metric: "lat", Query: "Q1", LT: ptr(0.5), Window: slo.WindowChaos},
			{Metric: "err", Query: "Q2", LT: ptr(0.01), Window: slo.WindowRecovery},
		},
		RawPromQL: "Q3",
		RawWindow: slo.WindowChaos,
	}
	fake := &prom.Fake{Values: map[string]float64{"Q1": 0.2, "Q2": 0.001, "Q3": 1}}
	p := window.Params{
		LoadStart:       time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC),
		ChaosStartAfter: 10 * time.Second,
		ChaosDuration:   30 * time.Second,
		LoadDuration:    120 * time.Second,
	}
	r, err := Evaluate(context.Background(), s, fake, p)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Overall {
		t.Fatalf("expected Pass, got %+v", r)
	}
	for _, tr := range r.Thresholds {
		if !tr.Passed {
			t.Errorf("threshold %s should pass: %+v", tr.Metric, tr)
		}
	}
	if !r.RawPromQLPass {
		t.Error("rawPromQL should pass")
	}
}

func TestEvaluateFailWhenThresholdExceeded(t *testing.T) {
	s := &slo.SLO{Thresholds: []slo.Threshold{
		{Metric: "lat", Query: "Q1", LT: ptr(0.5), Window: slo.WindowChaos},
	}}
	fake := &prom.Fake{Values: map[string]float64{"Q1": 0.9}}
	p := window.Params{
		LoadStart:       time.Now(),
		ChaosStartAfter: 10 * time.Second,
		ChaosDuration:   30 * time.Second,
		LoadDuration:    120 * time.Second,
	}
	r, err := Evaluate(context.Background(), s, fake, p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Overall {
		t.Fatalf("expected Fail, got Pass: %+v", r)
	}
}

func TestEvaluatePopulatesChaosWindow(t *testing.T) {
	loadStart := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	win := window.Params{
		LoadStart:       loadStart,
		ChaosStartAfter: 30 * time.Second,
		ChaosDuration:   60 * time.Second,
		LoadDuration:    180 * time.Second,
	}
	// Empty SLO — no thresholds, no raw PromQL. Evaluate should still
	// populate ChaosWindowStart and ChaosWindowEnd derived from win.
	s := &slo.SLO{}
	r, err := Evaluate(context.Background(), s, &prom.Fake{}, win)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	wantStart := loadStart.Add(30 * time.Second)
	wantEnd := wantStart.Add(60 * time.Second)
	if !r.ChaosWindowStart.Equal(wantStart) {
		t.Errorf("ChaosWindowStart = %v, want %v", r.ChaosWindowStart, wantStart)
	}
	if !r.ChaosWindowEnd.Equal(wantEnd) {
		t.Errorf("ChaosWindowEnd = %v, want %v", r.ChaosWindowEnd, wantEnd)
	}
}

func TestEvaluateRawPromQLFail(t *testing.T) {
	s := &slo.SLO{
		RawPromQL: "Q_fail",
		RawWindow: slo.WindowChaos,
	}
	// RawPromQL returns 0 (not 1) → RawPromQLPass=false → Overall=false.
	fake := &prom.Fake{Values: map[string]float64{"Q_fail": 0}}
	p := window.Params{
		LoadStart:       time.Now(),
		ChaosStartAfter: 10 * time.Second,
		ChaosDuration:   30 * time.Second,
		LoadDuration:    120 * time.Second,
	}
	r, err := Evaluate(context.Background(), s, fake, p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Overall {
		t.Error("expected Overall=false when rawPromQL returns 0")
	}
	if r.RawPromQLPass {
		t.Error("expected RawPromQLPass=false")
	}
}

func TestEvaluateQueryError(t *testing.T) {
	s := &slo.SLO{Thresholds: []slo.Threshold{
		{Metric: "lat", Query: "Q1", LT: ptr(0.5), Window: slo.WindowChaos},
	}}
	fake := &prom.FakeError{Err: errors.New("prom unavailable")}
	p := window.Params{
		LoadStart:       time.Now(),
		ChaosStartAfter: 10 * time.Second,
		ChaosDuration:   30 * time.Second,
		LoadDuration:    120 * time.Second,
	}
	_, err := Evaluate(context.Background(), s, fake, p)
	if err == nil {
		t.Error("expected error when QueryAt fails")
	}
}

func TestEvaluateGTBound(t *testing.T) {
	s := &slo.SLO{Thresholds: []slo.Threshold{
		{Metric: "throughput", Query: "Q1", GT: ptr(100), Window: slo.WindowChaos},
	}}
	fake := &prom.Fake{Values: map[string]float64{"Q1": 50}}
	p := window.Params{
		LoadStart: time.Now(), ChaosStartAfter: 10 * time.Second, ChaosDuration: 30 * time.Second, LoadDuration: 120 * time.Second,
	}
	r, _ := Evaluate(context.Background(), s, fake, p)
	if r.Overall {
		t.Fatal("50 > 100 is false, should fail")
	}
}
