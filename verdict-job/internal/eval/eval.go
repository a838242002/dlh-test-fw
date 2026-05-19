// Package eval combines threshold checks and raw PromQL into an overall result.
package eval

import (
	"context"
	"time"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/prom"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/slo"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/window"
)

type ThresholdResult struct {
	Metric      string   `json:"metric"`
	Query       string   `json:"query"`
	Window      string   `json:"window"`
	WindowStart string   `json:"window_start"`
	WindowEnd   string   `json:"window_end"`
	Value       float64  `json:"value"`
	LT          *float64 `json:"lt,omitempty"`
	GT          *float64 `json:"gt,omitempty"`
	Passed      bool     `json:"passed"`
}

type Result struct {
	Overall          bool              `json:"overall"`
	Thresholds       []ThresholdResult `json:"thresholds"`
	RawPromQL        string            `json:"raw_promql,omitempty"`
	RawPromQLValue   float64           `json:"raw_promql_value,omitempty"`
	RawPromQLPass    bool              `json:"raw_promql_pass,omitempty"`
	ChaosWindowStart time.Time         `json:"chaos_window_start"`
	ChaosWindowEnd   time.Time         `json:"chaos_window_end"`
}

func Evaluate(ctx context.Context, s *slo.SLO, p prom.API, win window.Params) (*Result, error) {
	r := &Result{
		Overall:          true,
		ChaosWindowStart: win.LoadStart.Add(win.ChaosStartAfter),
		ChaosWindowEnd:   win.LoadStart.Add(win.ChaosStartAfter).Add(win.ChaosDuration),
	}

	for _, t := range s.Thresholds {
		w, err := window.Compute(win, t.Window)
		if err != nil {
			return nil, err
		}
		v, err := p.QueryAt(ctx, t.Query, w.End)
		if err != nil {
			return nil, err
		}
		passed := false
		switch {
		case t.LT != nil:
			passed = v < *t.LT
		case t.GT != nil:
			passed = v > *t.GT
		}
		r.Thresholds = append(r.Thresholds, ThresholdResult{
			Metric: t.Metric, Query: t.Query, Window: string(t.Window),
			WindowStart: w.Start.UTC().Format("2006-01-02T15:04:05Z"),
			WindowEnd:   w.End.UTC().Format("2006-01-02T15:04:05Z"),
			Value: v, LT: t.LT, GT: t.GT, Passed: passed,
		})
		if !passed {
			r.Overall = false
		}
	}

	if s.RawPromQL != "" {
		w, err := window.Compute(win, s.RawWindow)
		if err != nil {
			return nil, err
		}
		v, err := p.QueryAt(ctx, s.RawPromQL, w.End)
		if err != nil {
			return nil, err
		}
		r.RawPromQL = s.RawPromQL
		r.RawPromQLValue = v
		r.RawPromQLPass = v == 1
		if !r.RawPromQLPass {
			r.Overall = false
		}
	}

	return r, nil
}
