package api

import (
	"testing"
	"time"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
)

func TestAddLinks_PopulatesArgoAndGrafana(t *testing.T) {
	h := &Handlers{deps: &Deps{Links: links.Config{
		ArgoBaseURL:    "https://argo.example.com",
		GrafanaBaseURL: "https://grafana.example.com",
		Namespace:      "dlh-test-fw",
	}}}
	wf := "mysql-pod-delete-20260523-130331"
	end := time.Date(2026, 5, 23, 13, 7, 47, 0, time.UTC)
	verdict := map[string]interface{}{"overall": true}
	d := gen.RunDetail{
		Id:           wf,
		Scenario:     "mysql-pod-delete",
		WorkflowName: &wf,
		StartedAt:    time.Date(2026, 5, 23, 13, 3, 31, 0, time.UTC),
		FinishedAt:   &end,
		Verdict:      &verdict, // a verdict means SLO/load metrics exist → Grafana links
	}
	h.addLinks(&d)

	if d.ArgoUrl == nil || *d.ArgoUrl != "https://argo.example.com/workflows/dlh-test-fw/"+wf {
		t.Errorf("ArgoUrl = %v", d.ArgoUrl)
	}
	if d.GrafanaUrls == nil || len(*d.GrafanaUrls) != 2 {
		t.Fatalf("want 2 grafana urls, got %v", d.GrafanaUrls)
	}
	if (*d.GrafanaUrls)[0].Label != "Run dashboard" || (*d.GrafanaUrls)[1].Label != "MySQL dashboard" {
		t.Errorf("labels = %v", *d.GrafanaUrls)
	}
}

func TestAddLinks_ChaosOnlyRunGetsArgoButNoGrafana(t *testing.T) {
	h := &Handlers{deps: &Deps{Links: links.Config{
		ArgoBaseURL:    "https://argo.example.com",
		GrafanaBaseURL: "https://grafana.example.com",
		Namespace:      "dlh-test-fw",
	}}}
	wf := "chaos-kafka-broker-partition-20260523-175130"
	end := time.Date(2026, 5, 23, 17, 53, 32, 0, time.UTC)
	// Bare chaos run: no Verdict, no Score → no k6/SLO metrics.
	d := gen.RunDetail{
		Id:           wf,
		Scenario:     "chaos-kafka-broker-partition",
		WorkflowName: &wf,
		StartedAt:    time.Date(2026, 5, 23, 17, 51, 30, 0, time.UTC),
		FinishedAt:   &end,
	}
	h.addLinks(&d)

	if d.ArgoUrl == nil {
		t.Errorf("expected Argo link for chaos run, got nil")
	}
	if d.GrafanaUrls != nil {
		t.Errorf("expected NO grafana links for chaos-only run, got %v", *d.GrafanaUrls)
	}
}

func TestAddLinks_NoConfigNoLinks(t *testing.T) {
	h := &Handlers{deps: &Deps{Links: links.Config{Namespace: "dlh-test-fw"}}}
	wf := "x"
	d := gen.RunDetail{Id: "x", Scenario: "mysql-pod-delete", WorkflowName: &wf, StartedAt: time.Now()}
	h.addLinks(&d)
	if d.ArgoUrl != nil || d.GrafanaUrls != nil {
		t.Errorf("expected no links, got argo=%v grafana=%v", d.ArgoUrl, d.GrafanaUrls)
	}
}
