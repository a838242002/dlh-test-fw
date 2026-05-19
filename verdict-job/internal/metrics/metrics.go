// Package metrics pushes a verdict summary to VictoriaMetrics so dashboards
// can render verdict state via PromQL alongside k6 metrics — no second
// datasource (Infinity → k8s API) required.
//
// Emits these series under labels `dlh_workflow` + `dlh_scenario`:
//
//	dlh_verdict_overall                   0 or 1
//	dlh_verdict_threshold_pass{name=...}  0 or 1, one per SLO threshold
//	dlh_verdict_threshold_value{name=...} the measured PromQL value
//
// Uses VictoriaMetrics' Prometheus text-import endpoint
// (`/api/v1/import/prometheus`) which is HTTP POST + line-based — no
// Prometheus remote-write client library needed.
package metrics

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
)

type Pusher struct {
	// URL is the full import endpoint, e.g.
	// "http://dlh-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428/api/v1/import/prometheus".
	URL    string
	Client *http.Client
}

func New(url string) *Pusher {
	return &Pusher{URL: url, Client: &http.Client{Timeout: 5 * time.Second}}
}

// Push serializes the verdict result as Prometheus text-format gauges
// labelled by workflow + scenario and POSTs them to VM. Returns nil on
// 2xx; the caller can treat a non-nil error as soft — the verdict run
// still succeeds, just without dashboard-visible summary.
func (p *Pusher) Push(ctx context.Context, workflow, scenario string, r *eval.Result) error {
	body := build(workflow, scenario, r)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("VM import returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func build(workflow, scenario string, r *eval.Result) []byte {
	var b strings.Builder
	base := fmt.Sprintf(`dlh_workflow=%q,dlh_scenario=%q`, workflow, scenario)
	fmt.Fprintf(&b, "dlh_verdict_overall{%s} %d\n", base, boolToInt(r.Overall))
	for _, t := range r.Thresholds {
		labels := fmt.Sprintf(`%s,name=%q`, base, t.Metric)
		fmt.Fprintf(&b, "dlh_verdict_threshold_pass{%s} %d\n", labels, boolToInt(t.Passed))
		fmt.Fprintf(&b, "dlh_verdict_threshold_value{%s} %g\n", labels, t.Value)
	}
	fmt.Fprintf(&b, "dlh_chaos_window_start_unixtime{%s} %d\n", base, r.ChaosWindowStart.Unix())
	fmt.Fprintf(&b, "dlh_chaos_window_end_unixtime{%s} %d\n", base, r.ChaosWindowEnd.Unix())
	return []byte(b.String())
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
