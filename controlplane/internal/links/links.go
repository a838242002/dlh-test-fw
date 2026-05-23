// Package links assembles outbound deep-link URLs (Argo Workflows UI, Grafana
// dashboards) for a run. All functions are pure; empty base URLs yield no link
// so the feature degrades gracefully when unconfigured.
package links

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config carries the per-environment base URLs + namespace used to build links.
type Config struct {
	ArgoBaseURL    string
	GrafanaBaseURL string
	Namespace      string
}

// NamedURL is a labeled outbound link.
type NamedURL struct {
	Label string
	URL   string
}

// runDashboardUID couples to dashboards/grafana/dlh-run-detail.json (FINDINGS #8).
// The dashboards' template variables are named `scenario` and `workflow`
// (populated from the dlh_scenario / dlh_workflow metric labels) — the deep link
// must set var-scenario + var-workflow, NOT var-dlh_scenario.
const runDashboardUID = "dlh-run"

var targetDashboards = map[string]struct{ uid, label string }{
	"mysql": {"dlh-mysql", "MySQL dashboard"},
	"kafka": {"dlh-kafka", "Kafka dashboard"},
	"doris": {"dlh-doris", "Doris dashboard"},
}

// DeriveTargetType infers the engine from a scenario id (heuristic — mirrors the
// web-side deriveTargetType). Returns "generic" when nothing matches.
func DeriveTargetType(scenario string) string {
	switch {
	case strings.Contains(scenario, "mysql"):
		return "mysql"
	case strings.Contains(scenario, "kafka"):
		return "kafka"
	case strings.Contains(scenario, "doris"):
		return "doris"
	default:
		return "generic"
	}
}

// ArgoURL builds a link to the Argo Workflows UI for a workflow, or "" if base
// or workflowName is empty.
func ArgoURL(base, namespace, workflowName string) string {
	if base == "" || workflowName == "" {
		return ""
	}
	return fmt.Sprintf("%s/workflows/%s/%s", strings.TrimRight(base, "/"), namespace, workflowName)
}

// GrafanaURLs builds the run dashboard link plus a per-target-type dashboard link
// (when recognized), scoped to the run's time window. The dashboards filter by the
// `scenario` + `workflow` template variables, so both are set. Returns nil if base
// is empty.
func GrafanaURLs(base, scenario, workflowName string, start time.Time, end *time.Time) []NamedURL {
	if base == "" {
		return nil
	}
	b := strings.TrimRight(base, "/")
	fromMs := strconv.FormatInt(start.UnixMilli(), 10)
	toPart := "now"
	if end != nil {
		toPart = strconv.FormatInt(end.UnixMilli(), 10)
	}
	sc := url.QueryEscape(scenario)
	wf := url.QueryEscape(workflowName)
	q := func(uid string) string {
		return fmt.Sprintf("%s/d/%s/%s?var-scenario=%s&var-workflow=%s&from=%s&to=%s", b, uid, uid, sc, wf, fromMs, toPart)
	}
	urls := []NamedURL{{Label: "Run dashboard", URL: q(runDashboardUID)}}
	if d, ok := targetDashboards[DeriveTargetType(scenario)]; ok {
		urls = append(urls, NamedURL{Label: d.label, URL: q(d.uid)})
	}
	return urls
}
