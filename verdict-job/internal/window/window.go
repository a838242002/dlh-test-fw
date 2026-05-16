// Package window computes the baseline/chaos/recovery/full windows of a scenario run.
package window

import (
	"fmt"
	"time"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/slo"
)

type Params struct {
	LoadStart       time.Time
	ChaosStartAfter time.Duration
	ChaosDuration   time.Duration
	LoadDuration    time.Duration
}

type Window struct {
	Start time.Time
	End   time.Time
}

func (p Params) Validate() error {
	if p.ChaosStartAfter < 0 || p.ChaosDuration <= 0 || p.LoadDuration <= 0 {
		return fmt.Errorf("window: all durations must be positive (chaos_start_after may be 0)")
	}
	if p.ChaosStartAfter+p.ChaosDuration > p.LoadDuration {
		return fmt.Errorf("window: chaos_start_after (%v) + chaos_duration (%v) > load_duration (%v)",
			p.ChaosStartAfter, p.ChaosDuration, p.LoadDuration)
	}
	return nil
}

func Compute(p Params, w slo.Window) (Window, error) {
	if err := p.Validate(); err != nil {
		return Window{}, err
	}
	chaosStart := p.LoadStart.Add(p.ChaosStartAfter)
	chaosEnd := chaosStart.Add(p.ChaosDuration)
	loadEnd := p.LoadStart.Add(p.LoadDuration)
	switch w {
	case slo.WindowBaseline:
		return Window{Start: p.LoadStart, End: chaosStart}, nil
	case slo.WindowChaos:
		return Window{Start: chaosStart, End: chaosEnd}, nil
	case slo.WindowRecovery:
		return Window{Start: chaosEnd, End: loadEnd}, nil
	case slo.WindowFull:
		return Window{Start: p.LoadStart, End: loadEnd}, nil
	default:
		return Window{}, fmt.Errorf("window: unknown window %q", w)
	}
}
