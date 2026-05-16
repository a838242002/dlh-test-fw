package slo

import (
	"os"
	"testing"
)

func TestParseSimple(t *testing.T) {
	b, err := os.ReadFile("../../testdata/slo-simple.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got, want := len(s.Thresholds), 2; got != want {
		t.Fatalf("Thresholds len: got %d want %d", got, want)
	}
	if s.Thresholds[0].Window != WindowChaos {
		t.Errorf("Thresholds[0].Window: got %q want %q", s.Thresholds[0].Window, WindowChaos)
	}
	if s.Thresholds[0].LT == nil || *s.Thresholds[0].LT != 0.5 {
		t.Errorf("Thresholds[0].LT: got %v want 0.5", s.Thresholds[0].LT)
	}
	if s.RawPromQL == "" || s.RawWindow != WindowChaos {
		t.Errorf("raw_promql/raw_window not parsed: %+v / %q", s.RawPromQL, s.RawWindow)
	}
}

func TestParseInvalidMissingBound(t *testing.T) {
	b, err := os.ReadFile("../../testdata/slo-invalid.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(b); err == nil {
		t.Fatalf("expected validation error, got nil")
	}
}

func TestParseRawPromQLRequiresWindow(t *testing.T) {
	yaml := []byte(`raw_promql: "up > 0"` + "\n")
	if _, err := Parse(yaml); err == nil {
		t.Fatalf("raw_promql without raw_window should fail validation")
	}
}
