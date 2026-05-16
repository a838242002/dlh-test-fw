package window

import (
	"testing"
	"time"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/slo"
)

func TestCompute(t *testing.T) {
	loadStart := time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC)
	p := Params{
		LoadStart:       loadStart,
		ChaosStartAfter: 30 * time.Second,
		ChaosDuration:   60 * time.Second,
		LoadDuration:    180 * time.Second,
	}

	cases := []struct {
		name       string
		w          slo.Window
		start, end time.Time
	}{
		{"baseline", slo.WindowBaseline, loadStart, loadStart.Add(30 * time.Second)},
		{"chaos", slo.WindowChaos, loadStart.Add(30 * time.Second), loadStart.Add(90 * time.Second)},
		{"recovery", slo.WindowRecovery, loadStart.Add(90 * time.Second), loadStart.Add(180 * time.Second)},
		{"full", slo.WindowFull, loadStart, loadStart.Add(180 * time.Second)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, err := Compute(p, c.w)
			if err != nil {
				t.Fatal(err)
			}
			if !w.Start.Equal(c.start) || !w.End.Equal(c.end) {
				t.Errorf("%s: got [%v,%v] want [%v,%v]", c.name, w.Start, w.End, c.start, c.end)
			}
		})
	}
}

func TestValidateRejectsChaosOverflow(t *testing.T) {
	p := Params{
		ChaosStartAfter: 60 * time.Second,
		ChaosDuration:   120 * time.Second,
		LoadDuration:    150 * time.Second,
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected validation error: chaos_start_after + chaos_duration > load_duration")
	}
}
