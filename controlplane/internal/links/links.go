// Package links assembles outbound deep-link URLs (Argo Workflows UI, Grafana
// dashboards) for a run. All functions are pure; empty base URLs yield no link
// so the feature degrades gracefully when unconfigured.
package links

import (
	"fmt"
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

// These couple to dashboards/grafana/ — keep in sync (FINDINGS #1, #8).
const (
	runDashboardUID = "dlh-run"
	scenarioVar     = "dlh_scenario"
)

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
// (when recognized), scoped to the run's time window. Returns nil if base is empty.
func GrafanaURLs(base, scenario string, start time.Time, end *time.Time) []NamedURL {
	if base == "" {
		return nil
	}
	b := strings.TrimRight(base, "/")
	fromMs := strconv.FormatInt(start.UnixMilli(), 10)
	toPart := "now"
	if end != nil {
		toPart = strconv.FormatInt(end.UnixMilli(), 10)
	}
	q := func(uid string) string {
		return fmt.Sprintf("%s/d/%s/%s?var-%s=%s&from=%s&to=%s", b, uid, uid, scenarioVar, scenario, fromMs, toPart)
	}
	urls := []NamedURL{{Label: "Run dashboard", URL: q(runDashboardUID)}}
	if d, ok := targetDashboards[DeriveTargetType(scenario)]; ok {
		urls = append(urls, NamedURL{Label: d.label, URL: q(d.uid)})
	}
	return urls
}
