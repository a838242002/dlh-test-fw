package runs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestManifest_JSONRoundtrip(t *testing.T) {
	startedAt := time.Date(2026, 5, 22, 10, 30, 0, 0, time.UTC)
	finishedAt := startedAt.Add(5 * time.Minute)
	score := 0.95
	m := Manifest{
		RunID:        "mysql-pod-delete-20260522-103000",
		Scenario:     "mysql-pod-delete",
		WorkflowName: "mysql-pod-delete-20260522-103000",
		Parameters:   map[string]string{"vus": "20"},
		CreatedBy:    "tester",
		Status:       "Succeeded",
		StartedAt:    startedAt,
		FinishedAt:   &finishedAt,
		Score:        &score,
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Manifest
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RunID != m.RunID || got.Status != "Succeeded" || got.Score == nil || *got.Score != 0.95 {
		t.Errorf("roundtrip lost data: %+v", got)
	}
}

func TestSanitize_NoSlashes(t *testing.T) {
	if got := sanitize("scenario/with/slashes"); got != "scenario_with_slashes" {
		t.Errorf("sanitize: %q", got)
	}
}

func TestWrite_NilClient_NoOp(t *testing.T) {
	w := &ManifestWriter{Client: nil, Bucket: "artifacts"}
	if err := w.Write(context.Background(), Manifest{RunID: "x"}); err != nil {
		t.Errorf("nil-client write should no-op, got %v", err)
	}
}

func TestManifest_HasTargetField(t *testing.T) {
	m := Manifest{
		RunID:    "x",
		Scenario: "mysql-pod-delete",
		Target:   "staging-mysql",
		Status:   "Running",
	}
	raw, _ := json.Marshal(m)
	if !strings.Contains(string(raw), `"target":"staging-mysql"`) {
		t.Errorf("target field missing in JSON: %s", raw)
	}
}
