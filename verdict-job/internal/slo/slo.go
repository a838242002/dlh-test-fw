// Package slo parses and validates SLO definitions from scenario YAML.
package slo

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

type Window string

const (
	WindowBaseline Window = "baseline"
	WindowChaos    Window = "chaos"
	WindowRecovery Window = "recovery"
	WindowFull     Window = "full"
)

func (w Window) Valid() bool {
	switch w {
	case WindowBaseline, WindowChaos, WindowRecovery, WindowFull:
		return true
	}
	return false
}

type Threshold struct {
	Metric string   `yaml:"metric"`
	Query  string   `yaml:"query"`
	LT     *float64 `yaml:"lt,omitempty"`
	GT     *float64 `yaml:"gt,omitempty"`
	Window Window   `yaml:"window"`
}

type SLO struct {
	Thresholds []Threshold `yaml:"thresholds"`
	RawPromQL  string      `yaml:"raw_promql,omitempty"`
	RawWindow  Window      `yaml:"raw_window,omitempty"`
}

func Parse(b []byte) (*SLO, error) {
	var s SLO
	if err := yaml.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *SLO) Validate() error {
	if len(s.Thresholds) == 0 && s.RawPromQL == "" {
		return errors.New("slo: at least one threshold or raw_promql required")
	}
	for i, t := range s.Thresholds {
		if t.Metric == "" {
			return fmt.Errorf("slo: threshold[%d].metric empty", i)
		}
		if t.Query == "" {
			return fmt.Errorf("slo: threshold[%d].query empty", i)
		}
		if (t.LT == nil) == (t.GT == nil) {
			return fmt.Errorf("slo: threshold[%d] (%s): exactly one of lt/gt required", i, t.Metric)
		}
		if !t.Window.Valid() {
			return fmt.Errorf("slo: threshold[%d] (%s): invalid window %q", i, t.Metric, t.Window)
		}
	}
	if s.RawPromQL != "" && !s.RawWindow.Valid() {
		return errors.New("slo: raw_window required (and valid) when raw_promql set")
	}
	return nil
}
