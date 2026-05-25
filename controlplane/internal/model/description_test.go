package model

import "testing"

func TestScenarioDescription_PrefersAnnotation(t *testing.T) {
	got := ScenarioDescription(
		map[string]string{"dlh.scenario/description": "Custom text."},
		"mysql-pod-delete", "mysql", "pod-delete")
	if got != "Custom text." {
		t.Fatalf("annotation should win, got %q", got)
	}
}

func TestScenarioDescription_DerivedFallback(t *testing.T) {
	got := ScenarioDescription(nil, "mysql-pod-delete", "mysql", "pod-delete")
	want := "pod-delete chaos on a mysql target, evaluated against the pod-delete SLO."
	if got != want {
		t.Fatalf("derived = %q, want %q", got, want)
	}
}
