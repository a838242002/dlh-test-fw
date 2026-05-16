package report

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
)

var update = flag.Bool("update", false, "rewrite golden files")

func sampleView() View {
	lt := 0.5
	return View{
		Result: &eval.Result{
			Overall:      true,
			ChaosVerdict: "Pass",
			Thresholds: []eval.ThresholdResult{{
				Metric: "p95-latency", Query: "Q1", Window: "chaos",
				WindowStart: "2026-05-16T10:00:30Z", WindowEnd: "2026-05-16T10:01:00Z",
				Value: 0.42, LT: &lt, Passed: true,
			}},
		},
		ScenarioName: "demo", LoadDurationSec: 120,
		GrafanaURL: "http://grafana/d/x", ArgoURL: "http://argo/wf",
	}
}

func TestRenderHTMLGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, sampleView()); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	p := filepath.Join("testdata", "golden-report.html")
	if *update {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("HTML mismatch (run -update to refresh).\nGot:\n%s", string(got))
	}
}

func TestRenderJSONShape(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, sampleView()); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{`"overall": true`, `"chaos_verdict": "Pass"`, `"metric": "p95-latency"`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON missing %q.\n%s", want, s)
		}
	}
}
