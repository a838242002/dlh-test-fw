package links

import (
	"strconv"
	"testing"
	"time"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestDeriveTargetType(t *testing.T) {
	cases := map[string]string{
		"mysql-pod-delete":         "mysql",
		"fixture-kafka-topic-seed": "kafka",
		"doris-be-network-loss":    "doris",
		"load-k6-run":              "generic",
	}
	for in, want := range cases {
		if got := DeriveTargetType(in); got != want {
			t.Errorf("DeriveTargetType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArgoURL(t *testing.T) {
	if got := ArgoURL("", "ns", "wf"); got != "" {
		t.Errorf("empty base should yield empty, got %q", got)
	}
	if got := ArgoURL("https://argo.example.com", "ns", ""); got != "" {
		t.Errorf("empty workflow should yield empty, got %q", got)
	}
	got := ArgoURL("https://argo.example.com/", "dlh-test-fw", "mysql-pod-delete-20260523-130331")
	want := "https://argo.example.com/workflows/dlh-test-fw/mysql-pod-delete-20260523-130331"
	if got != want {
		t.Errorf("ArgoURL = %q, want %q", got, want)
	}
}

func TestGrafanaURLs(t *testing.T) {
	if got := GrafanaURLs("", "mysql-pod-delete", "wf", time.Now(), nil); got != nil {
		t.Errorf("empty base should yield nil, got %v", got)
	}

	start := time.Date(2026, 5, 23, 13, 3, 31, 0, time.UTC)
	end := time.Date(2026, 5, 23, 13, 7, 47, 0, time.UTC)
	fromMs := start.UnixMilli()
	toMs := end.UnixMilli()
	wf := "mysql-pod-delete-20260523-130331"

	urls := GrafanaURLs("https://grafana.example.com/", "mysql-pod-delete", wf, start, &end)
	if len(urls) != 2 {
		t.Fatalf("want 2 urls, got %d (%v)", len(urls), urls)
	}
	if urls[0].Label != "Run dashboard" {
		t.Errorf("first label = %q, want Run dashboard", urls[0].Label)
	}
	wantRun := "https://grafana.example.com/d/dlh-run/dlh-run?var-scenario=mysql-pod-delete&var-workflow=" +
		wf + "&from=" + itoa(fromMs) + "&to=" + itoa(toMs)
	if urls[0].URL != wantRun {
		t.Errorf("run url = %q, want %q", urls[0].URL, wantRun)
	}
	if urls[1].Label != "MySQL dashboard" {
		t.Errorf("second label = %q, want MySQL dashboard", urls[1].Label)
	}
	wantMysql := "https://grafana.example.com/d/dlh-mysql/dlh-mysql?var-scenario=mysql-pod-delete&var-workflow=" +
		wf + "&from=" + itoa(fromMs) + "&to=" + itoa(toMs)
	if urls[1].URL != wantMysql {
		t.Errorf("mysql url = %q, want %q", urls[1].URL, wantMysql)
	}

	gen := GrafanaURLs("https://grafana.example.com", "load-k6-run", "load-k6-run-x", start, &end)
	if len(gen) != 1 {
		t.Errorf("generic want 1 url, got %d", len(gen))
	}

	run := GrafanaURLs("https://grafana.example.com", "mysql-pod-delete", wf, start, nil)
	if run[0].URL[len(run[0].URL)-7:] != "&to=now" {
		t.Errorf("running url should end with &to=now, got %q", run[0].URL)
	}
}
