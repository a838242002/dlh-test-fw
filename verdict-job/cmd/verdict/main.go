// Command verdict evaluates SLO thresholds and chaos verdict for a chaos+load run.
//
// Flags (kebab-case; Plan 4 WorkflowTemplate passes identical names):
//
//	-slo-yaml             path to SLO YAML
//	-chaos-result-name    ChaosResult CR name
//	-load-start-ts        RFC3339 timestamp of load start
//	-chaos-start-after    duration after load start when chaos begins
//	-chaos-duration       chaos duration
//	-load-duration        full load duration
//	-prom-url             VictoriaMetrics / Prometheus base URL
//	-prom-rw-url          VictoriaMetrics import endpoint (defaults to -prom-url + /api/v1/import/prometheus)
//	-scenario-label       dlh_scenario value embedded in pushed verdict metrics
//	-workflow-name        Argo workflow name (used for dlh-result-<workflow> ConfigMap)
//	-artifact-dir         where to write report.json / report.html
//	-namespace            namespace for ChaosResult + ConfigMap
//	-grafana-url          link embedded in report
//	-argo-url             link embedded in report
//	-chaos-verdict-timeout max wait for ChaosResult to leave Awaited
//
// Exits 0 (Pass) or 1 (Fail).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/chaosresult"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/metrics"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/prom"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/publish"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/report"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/slo"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/window"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type flags struct {
	sloPath             string
	chaosResultName     string
	loadStartTS         string
	chaosStartAfter     time.Duration
	chaosDuration       time.Duration
	loadDuration        time.Duration
	promURL             string
	promRwURL           string
	scenarioLabel       string
	workflowName        string
	artifactDir         string
	namespace           string
	grafanaURL          string
	argoURL             string
	chaosVerdictTimeout time.Duration
}

func parseFlags() flags {
	f := flags{}
	flag.StringVar(&f.sloPath, "slo-yaml", "", "path to SLO YAML")
	flag.StringVar(&f.chaosResultName, "chaos-result-name", "", "ChaosResult CR name")
	flag.StringVar(&f.loadStartTS, "load-start-ts", "", "RFC3339 timestamp of load start")
	flag.DurationVar(&f.chaosStartAfter, "chaos-start-after", 0, "duration after load start when chaos begins")
	flag.DurationVar(&f.chaosDuration, "chaos-duration", 0, "chaos duration")
	flag.DurationVar(&f.loadDuration, "load-duration", 0, "load duration")
	flag.StringVar(&f.promURL, "prom-url", "http://dlh-victoria-metrics-single-server.dlh-test-fw.svc.cluster.local:8428", "PromQL endpoint")
	flag.StringVar(&f.promRwURL, "prom-rw-url", "", "VictoriaMetrics import endpoint; if empty, derived from -prom-url + /api/v1/import/prometheus")
	flag.StringVar(&f.scenarioLabel, "scenario-label", "", "dlh_scenario value to embed in pushed verdict metrics")
	flag.StringVar(&f.workflowName, "workflow-name", "", "Argo workflow name (for ConfigMap)")
	flag.StringVar(&f.artifactDir, "artifact-dir", "/tmp/verdict", "where to write report.json / report.html")
	flag.StringVar(&f.namespace, "namespace", "dlh-test-fw", "namespace for ChaosResult + ConfigMap")
	flag.StringVar(&f.grafanaURL, "grafana-url", "", "link to embed in report")
	flag.StringVar(&f.argoURL, "argo-url", "", "link to embed in report")
	flag.DurationVar(&f.chaosVerdictTimeout, "chaos-verdict-timeout", 30*time.Second, "max wait for ChaosResult to leave Awaited")
	flag.Parse()
	return f
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		log.Fatalf("parse load-start-ts: %v", err)
	}
	return t
}

func main() {
	f := parseFlags()
	ctx := context.Background()

	sloBytes, err := os.ReadFile(f.sloPath)
	if err != nil {
		log.Fatalf("read SLO: %v", err)
	}
	s, err := slo.Parse(sloBytes)
	if err != nil {
		log.Fatalf("parse SLO: %v", err)
	}

	win := window.Params{
		LoadStart:       mustParseTime(f.loadStartTS),
		ChaosStartAfter: f.chaosStartAfter,
		ChaosDuration:   f.chaosDuration,
		LoadDuration:    f.loadDuration,
	}
	if err := win.Validate(); err != nil {
		log.Fatalf("window: %v", err)
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("k8s in-cluster config: %v", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("dynamic client: %v", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("kubernetes client: %v", err)
	}

	crClient := &chaosresult.Client{
		Dyn: dyn, Namespace: f.namespace,
		GVR: schema.GroupVersionResource{Group: "litmuschaos.io", Version: "v1alpha1", Resource: "chaosresults"},
	}
	chaosV, err := crClient.GetVerdict(ctx, f.chaosResultName, f.chaosVerdictTimeout)
	if err != nil {
		log.Fatalf("chaos verdict: %v", err)
	}

	p := prom.New(f.promURL)
	r, err := eval.Evaluate(ctx, s, p, win, chaosV)
	if err != nil {
		log.Fatalf("eval: %v", err)
	}

	view := report.View{
		Result:          r,
		ScenarioName:    f.workflowName,
		LoadDurationSec: int(f.loadDuration.Seconds()),
		GrafanaURL:      f.grafanaURL,
		ArgoURL:         f.argoURL,
		JSONURL:         "report.json",
	}
	jpath, hpath, err := report.Write(f.artifactDir, view)
	if err != nil {
		log.Fatalf("report: %v", err)
	}
	fmt.Printf("wrote %s and %s\n", jpath, hpath)

	pub := &publish.Publisher{Cs: cs, Namespace: f.namespace}
	if err := pub.Publish(ctx, f.workflowName, r); err != nil {
		log.Fatalf("publish: %v", err)
	}

	// Push the verdict summary as PromQL gauges so dashboards can render it
	// without a separate datasource. Soft-failure: log and continue — the
	// CM is the authoritative record.
	rwURL := f.promRwURL
	if rwURL == "" {
		rwURL = f.promURL + "/api/v1/import/prometheus"
	}
	if err := metrics.New(rwURL).Push(ctx, f.workflowName, f.scenarioLabel, r); err != nil {
		log.Printf("warn: failed to push verdict metrics to %s: %v", rwURL, err)
	} else {
		fmt.Printf("pushed verdict metrics to %s\n", rwURL)
	}

	if r.Overall {
		fmt.Println("VERDICT: PASS")
		os.Exit(0)
	}
	fmt.Println("VERDICT: FAIL")
	os.Exit(1)
}
