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
	d := gen.RunDetail{
		Id:           wf,
		Scenario:     "mysql-pod-delete",
		WorkflowName: &wf,
		StartedAt:    time.Date(2026, 5, 23, 13, 3, 31, 0, time.UTC),
		FinishedAt:   &end,
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

func TestAddLinks_NoConfigNoLinks(t *testing.T) {
	h := &Handlers{deps: &Deps{Links: links.Config{Namespace: "dlh-test-fw"}}}
	wf := "x"
	d := gen.RunDetail{Id: "x", Scenario: "mysql-pod-delete", WorkflowName: &wf, StartedAt: time.Now()}
	h.addLinks(&d)
	if d.ArgoUrl != nil || d.GrafanaUrls != nil {
		t.Errorf("expected no links, got argo=%v grafana=%v", d.ArgoUrl, d.GrafanaUrls)
	}
}
