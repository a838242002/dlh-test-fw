# Per-Target Dashboard Enrichment + Chaos Overlay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add 12 new panels across `dlh-mysql` / `dlh-kafka` / `dlh-doris` using k6 metrics already in VM, and add a Grafana chaos-window annotation source to all 4 per-run dashboards by emitting two new unix-epoch gauges from `verdict-job` at end-of-run.

**Architecture:** verdict-job's `eval.Result` gains two `time.Time` fields (chaos window start/end), already computable from existing `window.Params`. `metrics.build` emits two new Prometheus gauges (`dlh_chaos_window_start_unixtime`, `dlh_chaos_window_end_unixtime`) labelled by `dlh_workflow` + `dlh_scenario`. Each of the 4 per-run dashboards gains an `annotations.list` block using Grafana's `useValueForTime: true` to render vertical marks AT the gauge's value-as-timestamp. Three of the four dashboards (mysql, kafka, doris) gain extra timeseries panels using metrics already in VM (`k6_vus`, `k6_data_*_total`, latency percentile siblings, kafka writer internals).

**Tech Stack:** Go 1.26 (verdict-job), Prometheus text-import endpoint to VictoriaMetrics, Grafana 11.x (bundled by chart 8.15.0), JSON dashboards, Helm 4.

**Reference spec:** `docs/superpowers/specs/2026-05-20-dashboard-enrichment-design.md`. Re-read the architecture diagram, the per-target panel additions, the chaos overlay mechanism, and the risk register before starting.

**Branch & worktree:** Per `CLAUDE.md` conventions, create a feature worktree at `../dlh-test-fw-plan13` on branch `feat/plan13-dashboard-enrichment` before Task 2. Task 1 runs from the main worktree.

---

## File Structure

**New files:** none.

**Modified files (Go):**
- `verdict-job/internal/eval/eval.go` — add `ChaosWindowStart`/`ChaosWindowEnd` fields to `Result`; populate them in `Evaluate`.
- `verdict-job/internal/metrics/metrics.go` — extend `build()` to emit the two new gauges from `r.ChaosWindowStart` / `r.ChaosWindowEnd`.
- `verdict-job/internal/metrics/metrics_test.go` — extend fixture + assertions for new gauges.
- (no signature change to `Push` or `main.go` — new gauges are derived from the existing `*eval.Result`.)

**Modified files (Dashboards):**
- `dashboards/grafana/dlh-mysql.json` — +3 panels + annotations source.
- `dashboards/grafana/dlh-kafka.json` — +6 panels + annotations source.
- `dashboards/grafana/dlh-doris.json` — +3 panels + annotations source.
- `dashboards/grafana/dlh-run-detail.json` — annotations source only.
- `helm/dlh-test-fw/files/dashboards/*.json` — synced via `make sync-dashboards` (4 files in lockstep).

**Modified files (Docs):**
- `docs/FINDINGS.md` — append Plan 13 section.

**Unchanged:** chart deps, WorkflowTemplates, scenarios, scripts, RBAC, dlh-k6 image.

---

## Task 1: Baseline + Grafana version check

This task makes no commits. Confirms cluster is healthy AFTER yesterday's restart, confirms verdict-job baseline tests pass, and checks Grafana version compatibility with the `useValueForTime: true` annotation feature (needed for the chaos overlay).

**Files:** None modified.

Work from: `/Users/allen/repo/dlh-test-fw` (main worktree, branch `main`).

- [ ] **Step 1: Confirm clean tree + recent state**

```bash
git status
git log --first-parent --oneline -5
```

Expected: clean tree on `main`; HEAD includes `7554070` (Plan 13 spec) or newer.

- [ ] **Step 2: Plan 12 baseline still green**

```bash
make run-mysql
```

Use Bash timeout ≥ 600000 ms (10 min). Expected: `Final phase: Succeeded`. If `Failed`, fix before continuing — this plan layers on top.

- [ ] **Step 3: Confirm Grafana version supports `useValueForTime`**

```bash
kubectl -n dlh-test-fw get deploy dlh-grafana \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
```

Expected: image tag `grafana:11.x.y` (any Grafana 11). `useValueForTime` on Prometheus annotations is documented since Grafana 9.0; v11 is fine. If the image is < v9, STOP and report BLOCKED — annotation rendering won't work.

- [ ] **Step 4: Confirm verdict-job tests pass on `main`**

```bash
cd verdict-job
go vet ./...
go test ./...
cd -
```

Expected: all 6 packages PASS. If any fails, fix or report BLOCKED before continuing.

- [ ] **Step 5: Inventory current panel counts (sanity for later)**

```bash
for f in dashboards/grafana/dlh-mysql.json dashboards/grafana/dlh-kafka.json dashboards/grafana/dlh-doris.json dashboards/grafana/dlh-run-detail.json; do
  c=$(python3 -c "import json; print(len(json.load(open('$f'))['panels']))")
  echo "$f: $c panels"
done
```

Expected: dlh-mysql 5, dlh-kafka 5, dlh-doris 5, dlh-run-detail 5. (Post-Plan-12 baseline.)

- [ ] **Step 6: Confirm no commits**

```bash
git status
```

Expected: clean (untracked plan-doc file is OK).

---

## Task 2: Worktree + eval.Result chaos-window fields (TDD)

**Files:**
- Create worktree at `../dlh-test-fw-plan13`
- Modify: `verdict-job/internal/eval/eval.go`
- Modify: `verdict-job/internal/eval/eval_test.go` (add new field assertions)

- [ ] **Step 1: Create worktree**

From `/Users/allen/repo/dlh-test-fw`:

```bash
git worktree add ../dlh-test-fw-plan13 -b feat/plan13-dashboard-enrichment main
cd ../dlh-test-fw-plan13
git status
```

Expected: on `feat/plan13-dashboard-enrichment`, working tree clean.

All subsequent steps operate from `/Users/allen/repo/dlh-test-fw-plan13`.

- [ ] **Step 2: Read current eval_test.go shape**

```bash
cat verdict-job/internal/eval/eval_test.go
```

Note the existing test structure (fixtures + `Evaluate` calls + assertions). We extend rather than rewrite.

- [ ] **Step 3: Write a failing test asserting the new fields**

Add a test function to `verdict-job/internal/eval/eval_test.go`. Pick a name that doesn't collide with existing tests:

```go
func TestEvaluatePopulatesChaosWindow(t *testing.T) {
	loadStart := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	win := window.Params{
		LoadStart:       loadStart,
		ChaosStartAfter: 30 * time.Second,
		ChaosDuration:   60 * time.Second,
		LoadDuration:    180 * time.Second,
	}
	// Empty SLO — no thresholds, no raw PromQL. Evaluate should still
	// populate ChaosWindowStart and ChaosWindowEnd derived from win.
	s := &slo.SLO{}
	r, err := eval.Evaluate(context.Background(), s, fakeProm{}, win)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	wantStart := loadStart.Add(30 * time.Second)
	wantEnd := wantStart.Add(60 * time.Second)
	if !r.ChaosWindowStart.Equal(wantStart) {
		t.Errorf("ChaosWindowStart = %v, want %v", r.ChaosWindowStart, wantStart)
	}
	if !r.ChaosWindowEnd.Equal(wantEnd) {
		t.Errorf("ChaosWindowEnd = %v, want %v", r.ChaosWindowEnd, wantEnd)
	}
}
```

Required additional imports at top of file (only if not already there):

```go
import (
	"context"
	"testing"
	"time"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/slo"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/window"
)
```

And a fake prom client if not already defined in the test file:

```go
type fakeProm struct{}

func (fakeProm) QueryAt(_ context.Context, _ string, _ time.Time) (float64, error) {
	return 0, nil
}
```

If `eval_test.go` already defines a fake prom type with a different name, reuse that name and skip the new declaration.

- [ ] **Step 4: Run test — must fail (fields don't exist yet)**

```bash
cd verdict-job
go test ./internal/eval -run TestEvaluatePopulatesChaosWindow -v
cd -
```

Expected: FAIL with `r.ChaosWindowStart undefined (type *eval.Result has no field or method ChaosWindowStart)` or similar.

- [ ] **Step 5: Implement — add fields to Result + populate in Evaluate**

Edit `verdict-job/internal/eval/eval.go`. Two changes:

**5a.** Extend the `Result` struct:

```go
type Result struct {
	Overall          bool              `json:"overall"`
	Thresholds       []ThresholdResult `json:"thresholds"`
	RawPromQL        string            `json:"raw_promql,omitempty"`
	RawPromQLValue   float64           `json:"raw_promql_value,omitempty"`
	RawPromQLPass    bool              `json:"raw_promql_pass,omitempty"`
	ChaosWindowStart time.Time         `json:"chaos_window_start"`
	ChaosWindowEnd   time.Time         `json:"chaos_window_end"`
}
```

Required additional import if `time` isn't already imported in `eval.go`:

```go
import (
	"context"
	"time"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/prom"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/slo"
	"github.com/dlh/dlh-test-fw/verdict-job/internal/window"
)
```

**5b.** Populate the fields at the top of `Evaluate`, just after `r := &Result{Overall: true}`:

```go
func Evaluate(ctx context.Context, s *slo.SLO, p prom.API, win window.Params) (*Result, error) {
	r := &Result{
		Overall:          true,
		ChaosWindowStart: win.LoadStart.Add(win.ChaosStartAfter),
		ChaosWindowEnd:   win.LoadStart.Add(win.ChaosStartAfter).Add(win.ChaosDuration),
	}

	for _, t := range s.Thresholds {
		// ... unchanged body ...
	}
	// ... unchanged rest ...
	return r, nil
}
```

- [ ] **Step 6: Run the new test — must pass**

```bash
cd verdict-job
go test ./internal/eval -run TestEvaluatePopulatesChaosWindow -v
cd -
```

Expected: PASS.

- [ ] **Step 7: Run all verdict-job tests to confirm no regressions**

```bash
cd verdict-job
go vet ./...
go test ./...
cd -
```

Expected: every package PASS. If `metrics_test.go` fails because it constructs `*eval.Result` literals that now miss fields (Go's zero-value default makes this unlikely), fix by leaving the new fields zero — they'll be the time.Time zero value, which is fine for those existing tests.

- [ ] **Step 8: Commit**

```bash
git add verdict-job/internal/eval/eval.go verdict-job/internal/eval/eval_test.go
git commit -m "feat(verdict/eval): expose ChaosWindow{Start,End} on Result

Plan 13 step 1: eval.Result gains two time.Time fields derived from the
existing window.Params (LoadStart + ChaosStartAfter, plus ChaosDuration).
metrics.Push will emit them as unix-epoch gauges in Task 3, where they
become the data source for the Grafana chaos-overlay annotation."
```

---

## Task 3: metrics.build emits chaos-window gauges (TDD)

**Files:**
- Modify: `verdict-job/internal/metrics/metrics.go`
- Modify: `verdict-job/internal/metrics/metrics_test.go`

- [ ] **Step 1: Extend the existing TestPushSerializesAndPOSTs to assert the new lines**

Open `verdict-job/internal/metrics/metrics_test.go`. Modify the existing `TestPushSerializesAndPOSTs` function:

**1a.** Add `ChaosWindowStart` + `ChaosWindowEnd` to the `*eval.Result` fixture. Pick distinct unix-epoch timestamps so it's obvious the gauges carry the right values:

```go
r := &eval.Result{
	Overall: false,
	Thresholds: []eval.ThresholdResult{
		{Metric: "http_5xx_rate", Value: 0, Passed: true},
		{Metric: "p95_latency_ms", Value: 12.5, Passed: false},
	},
	ChaosWindowStart: time.Unix(1779210000, 0),
	ChaosWindowEnd:   time.Unix(1779210060, 0),
}
```

Required additional import in the test file (only if not present):

```go
"time"
```

**1b.** Extend `wantLines` with two new entries:

```go
wantLines := []string{
	`dlh_verdict_overall{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete"} 0`,
	`dlh_verdict_threshold_pass{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete",name="http_5xx_rate"} 1`,
	`dlh_verdict_threshold_value{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete",name="http_5xx_rate"} 0`,
	`dlh_verdict_threshold_pass{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete",name="p95_latency_ms"} 0`,
	`dlh_verdict_threshold_value{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete",name="p95_latency_ms"} 12.5`,
	`dlh_chaos_window_start_unixtime{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete"} 1779210000`,
	`dlh_chaos_window_end_unixtime{dlh_workflow="wf-123",dlh_scenario="mysql-pod-delete"} 1779210060`,
}
```

- [ ] **Step 2: Run test — must fail (gauges not emitted yet)**

```bash
cd verdict-job
go test ./internal/metrics -run TestPushSerializesAndPOSTs -v
cd -
```

Expected: FAIL with `missing line in payload: dlh_chaos_window_start_unixtime{...} 1779210000`.

- [ ] **Step 3: Extend `build()` to emit the new gauges**

Edit `verdict-job/internal/metrics/metrics.go`. In the `build` function, add two more `fmt.Fprintf` lines after the threshold loop, before `return`:

```go
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
```

Note: `r.ChaosWindowStart.Unix()` returns int64. Format with `%d`. If `ChaosWindowStart` is the time.Time zero value (e.g. an old test fixture without the field set), `.Unix()` returns `-6795364578871` — that's fine for tests, won't blow up.

- [ ] **Step 4: Run test — must pass**

```bash
cd verdict-job
go test ./internal/metrics -run TestPushSerializesAndPOSTs -v
cd -
```

Expected: PASS.

- [ ] **Step 5: Run all verdict-job tests**

```bash
cd verdict-job
go vet ./...
go test ./...
cd -
```

Expected: all 6 packages PASS.

- [ ] **Step 6: Commit**

```bash
git add verdict-job/internal/metrics/metrics.go verdict-job/internal/metrics/metrics_test.go
git commit -m "feat(verdict/metrics): emit chaos window unix-epoch gauges

Adds dlh_chaos_window_start_unixtime + dlh_chaos_window_end_unixtime,
labelled by dlh_workflow + dlh_scenario, sourced from the new
ChaosWindow{Start,End} fields on eval.Result. Two new lines in build();
test extended with two new wantLines."
```

---

## Task 4: Build + reload verdict image; live-verify the new gauges

**Files:** None modified. Verification only.

- [ ] **Step 1: Build + load the new verdict image into minikube**

```bash
cd verdict-job
make image
make load-image
cd -
```

Note: per FINDINGS.md the Makefile builds `dlh-verdict:0.1.0` but values.yaml references `ghcr.io/dlh/dlh-verdict:0.1.0`. The Plan 12 implementer worked around this by re-tagging. If `make load-image` doesn't load the `ghcr.io/dlh/...` tag, do:

```bash
docker tag dlh-verdict:0.1.0 ghcr.io/dlh/dlh-verdict:0.1.0
minikube image load ghcr.io/dlh/dlh-verdict:0.1.0
```

Confirm the image is in minikube:

```bash
minikube ssh -- docker images | grep dlh-verdict
```

- [ ] **Step 2: Run a fresh mysql scenario**

```bash
make run-mysql
```

Bash timeout ≥ 600000 ms (10 min). Expected: `Final phase: Succeeded`.

- [ ] **Step 3: Verify the new gauges are visible in VM**

```bash
wf=$(kubectl -n dlh-test-fw get workflow -o name | grep -E 'mysql-pod-delete-[0-9]{8}-[0-9]{6}$' | sort | tail -1 | sed 's|.*/||')
echo "wf=$wf"
kubectl -n dlh-test-fw port-forward svc/dlh-victoria-metrics-single-server 8428:8428 >/tmp/pf.log 2>&1 &
PF=$!
sleep 5
for g in dlh_chaos_window_start_unixtime dlh_chaos_window_end_unixtime; do
  v=$(curl -s "http://localhost:8428/api/v1/query?query=${g}%7Bdlh_workflow%3D%22${wf}%22%7D" \
        | python3 -c "import sys,json; r=json.load(sys.stdin)['data']['result']; print(r[0]['value'][1] if r else 'NONE')")
  echo "  $g = $v"
done
kill $PF 2>/dev/null
```

Expected: both gauges return a unix-epoch value (10-digit number around `1779...`). If either is `NONE`, the verdict pod didn't pick up the new image — re-check Step 1's image-load. If both return values but they're nonsensical (e.g. epoch year ~1970), the `Unix()` conversion is wrong — re-check Task 3 Step 3.

- [ ] **Step 4: Sanity-check the timestamps match the workflow window**

```bash
start=$(kubectl -n dlh-test-fw get workflow "$wf" -o jsonpath='{.status.startedAt}')
echo "workflow startedAt=$start"
# chaos_start_after for mysql is 30s; chaos_duration is 60s.
# So expected:  ChaosWindowStart ≈ startedAt + 30s
#               ChaosWindowEnd   ≈ startedAt + 30s + 60s = startedAt + 90s
# However eval uses LoadStart (the load step's startedAt), not workflow startedAt — they differ by load-fixture + prep-table latency, typically ~30-60s.
```

Roughly: ChaosWindowStart should be within the workflow run window. If the gauges return epochs FAR before or after the workflow's actual run time, something is wrong with Task 2's `Evaluate` change.

- [ ] **Step 5: No commit**

Verification task; no source changes. If anything failed, return to Task 2 or 3.

---

## Task 5: Chaos overlay annotations on all 4 per-run dashboards

**Files:**
- Modify: `dashboards/grafana/dlh-mysql.json`
- Modify: `dashboards/grafana/dlh-kafka.json`
- Modify: `dashboards/grafana/dlh-doris.json`
- Modify: `dashboards/grafana/dlh-run-detail.json`
- Modify (synced): `helm/dlh-test-fw/files/dashboards/*.json`

- [ ] **Step 1: Add `annotations.list` block to all 4 dashboards via a Python script**

Save the following as `/tmp/add-chaos-annotations.py`:

```python
import json
import sys

ANNOTATIONS = {
    "list": [
        {
            "name": "chaos start",
            "datasource": {"type": "prometheus", "uid": "VictoriaMetrics"},
            "expr": 'dlh_chaos_window_start_unixtime{dlh_workflow="$workflow"}',
            "useValueForTime": True,
            "iconColor": "orange",
            "tagKeys": "",
            "titleFormat": "chaos start",
            "enable": True,
        },
        {
            "name": "chaos recovered",
            "datasource": {"type": "prometheus", "uid": "VictoriaMetrics"},
            "expr": 'dlh_chaos_window_end_unixtime{dlh_workflow="$workflow"}',
            "useValueForTime": True,
            "iconColor": "green",
            "tagKeys": "",
            "titleFormat": "chaos recovered",
            "enable": True,
        },
    ]
}

for path in sys.argv[1:]:
    with open(path) as f:
        d = json.load(f)
    d["annotations"] = ANNOTATIONS
    with open(path, "w") as f:
        json.dump(d, f, indent=2)
        f.write("\n")
    print(f"updated {path}")
```

Run it on all four dashboards:

```bash
python3 /tmp/add-chaos-annotations.py \
  dashboards/grafana/dlh-mysql.json \
  dashboards/grafana/dlh-kafka.json \
  dashboards/grafana/dlh-doris.json \
  dashboards/grafana/dlh-run-detail.json
```

Expected output: 4 `updated ...` lines.

- [ ] **Step 2: Sync to chart**

```bash
make sync-dashboards
```

Expected: `cp dashboards/grafana/*.json helm/dlh-test-fw/files/dashboards/`.

- [ ] **Step 3: helm lint + deploy**

```bash
helm lint helm/dlh-test-fw
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw --timeout 3m
```

Expected: lint clean; upgrade succeeds.

- [ ] **Step 4: Verify the JSON annotation blocks are well-formed**

```bash
for f in dashboards/grafana/dlh-mysql.json dashboards/grafana/dlh-kafka.json dashboards/grafana/dlh-doris.json dashboards/grafana/dlh-run-detail.json; do
  python3 -c "
import json
d = json.load(open('$f'))
ann = d.get('annotations', {}).get('list', [])
names = [a['name'] for a in ann]
print('$f:', names)
"
done
```

Expected: each line lists `['chaos start', 'chaos recovered']`.

- [ ] **Step 5: Wait for Grafana sidecar to reload, then visually verify**

```bash
kubectl -n dlh-test-fw rollout status deploy/dlh-grafana --timeout=60s
sleep 30  # grafana-sidecar polls every ~30s
```

Open Grafana (`kubectl -n dlh-test-fw port-forward svc/dlh-grafana 3001:80 &`), navigate to dlh-mysql, select the latest workflow from the Workflow dropdown. EXPECTED:
- Two annotation toggles in the top bar: `chaos start` (orange icon), `chaos recovered` (green icon), both enabled.
- A vertical orange mark at the chaos-window start time appears across each timeseries panel.
- A vertical green mark at the chaos-window end time.

If marks DON'T appear:
- Open browser devtools → Network → look for the annotation query response. If `result: []`, the gauge query returned nothing — re-check Task 4's gauge verification.
- If response has data but mark not rendered, `useValueForTime` may be parsed differently — check Grafana logs.

(This step is human-visual. Other tasks can proceed if the JSON is valid; this is the final UX check.)

- [ ] **Step 6: Commit**

```bash
git add dashboards/grafana/ helm/dlh-test-fw/files/dashboards/
git commit -m "feat(dashboards): add chaos-window annotations source to 4 per-run dashboards

Each dashboard (dlh-mysql, dlh-kafka, dlh-doris, dlh-run-detail) gains an
annotations.list block with two Prometheus-backed entries pointing at
dlh_chaos_window_start_unixtime + dlh_chaos_window_end_unixtime, using
useValueForTime: true to render vertical marks AT the gauge-value-as-
timestamp (not at the metric sample's own timestamp). Orange icon for
'chaos start', green for 'chaos recovered'."
```

---

## Task 6: dlh-mysql — add row 2 (VUs, Query latency percentiles, Data throughput)

**Files:**
- Modify: `dashboards/grafana/dlh-mysql.json`
- Modify (synced): `helm/dlh-test-fw/files/dashboards/dlh-mysql.json`

- [ ] **Step 1: Write a Python script to inject the 3 new panels**

Save as `/tmp/add-mysql-row2.py`:

```python
import json

PATH = "dashboards/grafana/dlh-mysql.json"
DS = {"type": "prometheus", "uid": "VictoriaMetrics"}

PANELS = [
    {
        "type": "timeseries",
        "title": "VUs over time",
        "description": "k6 virtual user count vs configured max. Shows load ramp profile.",
        "targets": [
            {"expr": 'k6_vus{dlh_workflow="$workflow"}',     "datasource": DS, "legendFormat": "current"},
            {"expr": 'k6_vus_max{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "max"},
        ],
        "gridPos": {"x": 0, "y": 8, "w": 8, "h": 8},
    },
    {
        "type": "timeseries",
        "title": "Query latency percentiles",
        "description": "mysql query latency distribution from xk6-sql.",
        "targets": [
            {"expr": 'k6_dlh_mysql_query_duration_seconds_avg{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "avg"},
            {"expr": 'k6_dlh_mysql_query_duration_seconds_p95{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "p95"},
            {"expr": 'k6_dlh_mysql_query_duration_seconds_p99{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "p99"},
            {"expr": 'k6_dlh_mysql_query_duration_seconds_max{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "max"},
        ],
        "fieldConfig": {"defaults": {"unit": "s"}},
        "gridPos": {"x": 8, "y": 8, "w": 8, "h": 8},
    },
    {
        "type": "timeseries",
        "title": "Data throughput",
        "description": "k6 total bytes sent/received per second on the wire.",
        "targets": [
            {"expr": 'rate(k6_data_sent_total{dlh_workflow="$workflow"}[30s])',     "datasource": DS, "legendFormat": "sent"},
            {"expr": 'rate(k6_data_received_total{dlh_workflow="$workflow"}[30s])', "datasource": DS, "legendFormat": "received"},
        ],
        "fieldConfig": {"defaults": {"unit": "Bps"}},
        "gridPos": {"x": 16, "y": 8, "w": 8, "h": 8},
    },
]

with open(PATH) as f:
    d = json.load(f)

# Shift existing y>=8 panels (Verdict-overall, SLO table) down by 8 to make room.
for p in d["panels"]:
    gp = p.get("gridPos", {})
    if gp.get("y", 0) >= 8:
        gp["y"] += 8

d["panels"].extend(PANELS)

with open(PATH, "w") as f:
    json.dump(d, f, indent=2)
    f.write("\n")
print(f"{PATH}: {len(d['panels'])} panels total")
```

Run it:

```bash
python3 /tmp/add-mysql-row2.py
```

Expected: `dashboards/grafana/dlh-mysql.json: 8 panels total`.

- [ ] **Step 2: Sync + lint + deploy**

```bash
make sync-dashboards
helm lint helm/dlh-test-fw
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw --timeout 3m
```

Expected: lint clean; upgrade succeeds.

- [ ] **Step 3: Verify panel data via PromQL probe**

```bash
wf=$(kubectl -n dlh-test-fw get workflow -o name | grep -E 'mysql-pod-delete-[0-9]{8}-[0-9]{6}$' | sort | tail -1 | sed 's|.*/||')
kubectl -n dlh-test-fw port-forward svc/dlh-victoria-metrics-single-server 8428:8428 >/tmp/pf.log 2>&1 &
PF=$!
sleep 5
start=$(kubectl -n dlh-test-fw get workflow "$wf" -o jsonpath='{.status.startedAt}')
finish=$(kubectl -n dlh-test-fw get workflow "$wf" -o jsonpath='{.status.finishedAt}')
s=$(TZ=UTC date -j -f "%Y-%m-%dT%H:%M:%SZ" "$start" +%s)
f=$(TZ=UTC date -j -f "%Y-%m-%dT%H:%M:%SZ" "$finish" +%s)
echo "wf=$wf window=$s..$f"
for label in vus latency-p95 sent-rate; do
  case "$label" in
    vus)         q="k6_vus%7Bdlh_workflow%3D%22${wf}%22%7D" ;;
    latency-p95) q="k6_dlh_mysql_query_duration_seconds_p95%7Bdlh_workflow%3D%22${wf}%22%7D" ;;
    sent-rate)   q="rate(k6_data_sent_total%7Bdlh_workflow%3D%22${wf}%22%7D%5B30s%5D)" ;;
  esac
  r=$(curl -s "http://localhost:8428/api/v1/query_range?query=${q}&start=${s}&end=${f}&step=15")
  c=$(echo "$r" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['data']['result']))")
  printf "  %-12s c=%s\n" "$label" "$c"
done
kill $PF 2>/dev/null
```

Expected: at least 1 series for each of the three probes.

- [ ] **Step 4: Commit**

```bash
git add dashboards/grafana/dlh-mysql.json helm/dlh-test-fw/files/dashboards/dlh-mysql.json
git commit -m "feat(dashboards/dlh-mysql): add row 2 — VUs, latency percentiles, data throughput

Three new timeseries panels at y=8 (each w=8) using k6 metrics already in
VM. Existing Verdict-overall + SLO table panels shifted from y=8/13 to
y=16/21 to make room. Total panels: 5 -> 8."
```

---

## Task 7: dlh-kafka — add row 2 (generic) + row 3 (xk6-kafka writer internals)

**Files:**
- Modify: `dashboards/grafana/dlh-kafka.json`
- Modify (synced): `helm/dlh-test-fw/files/dashboards/dlh-kafka.json`

- [ ] **Step 1: Inject 6 new panels via Python**

Save as `/tmp/add-kafka-rows.py`:

```python
import json

PATH = "dashboards/grafana/dlh-kafka.json"
DS = {"type": "prometheus", "uid": "VictoriaMetrics"}

ROW2 = [
    {
        "type": "timeseries",
        "title": "VUs over time",
        "description": "k6 virtual user count vs configured max.",
        "targets": [
            {"expr": 'k6_vus{dlh_workflow="$workflow"}',     "datasource": DS, "legendFormat": "current"},
            {"expr": 'k6_vus_max{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "max"},
        ],
        "gridPos": {"x": 0, "y": 8, "w": 8, "h": 8},
    },
    {
        "type": "timeseries",
        "title": "Iteration p95",
        "description": "Generic k6 per-iteration latency. Includes the full kafka.js loop, not just produce. Unit: ms (k6 default).",
        "targets": [
            {"expr": 'k6_iteration_duration_p95{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "p95"},
            {"expr": 'k6_iteration_duration_p99{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "p99"},
        ],
        "fieldConfig": {"defaults": {"unit": "ms"}},
        "gridPos": {"x": 8, "y": 8, "w": 8, "h": 8},
    },
    {
        "type": "timeseries",
        "title": "Data throughput",
        "description": "k6 total bytes sent/received per second on the wire.",
        "targets": [
            {"expr": 'rate(k6_data_sent_total{dlh_workflow="$workflow"}[30s])',     "datasource": DS, "legendFormat": "sent"},
            {"expr": 'rate(k6_data_received_total{dlh_workflow="$workflow"}[30s])', "datasource": DS, "legendFormat": "received"},
        ],
        "fieldConfig": {"defaults": {"unit": "Bps"}},
        "gridPos": {"x": 16, "y": 8, "w": 8, "h": 8},
    },
]

ROW3 = [
    {
        "type": "timeseries",
        "title": "Writer batch queue p95",
        "description": "Time messages sit in xk6-kafka's writer batch queue before flush. High = client struggling to drain.",
        "targets": [
            {"expr": 'k6_kafka_writer_batch_queue_seconds_p95{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "p95"},
            {"expr": 'k6_kafka_writer_batch_queue_seconds_p99{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "p99"},
        ],
        "fieldConfig": {"defaults": {"unit": "s"}},
        "gridPos": {"x": 0, "y": 16, "w": 8, "h": 8},
    },
    {
        "type": "timeseries",
        "title": "Writer retries + errors",
        "description": "xk6-kafka writer retry attempts and terminal error count. Non-zero during chaos is expected.",
        "targets": [
            {"expr": 'rate(k6_kafka_writer_retries_count_total{dlh_workflow="$workflow"}[30s])', "datasource": DS, "legendFormat": "retries/s"},
            {"expr": 'rate(k6_kafka_writer_error_count_total{dlh_workflow="$workflow"}[30s])',   "datasource": DS, "legendFormat": "errors/s"},
        ],
        "gridPos": {"x": 8, "y": 16, "w": 8, "h": 8},
    },
    {
        "type": "timeseries",
        "title": "Writer batch bytes/sec",
        "description": "Bytes/sec written by xk6-kafka writer (batched). Compare with overall data throughput.",
        "targets": [
            {"expr": 'rate(k6_kafka_writer_batch_bytes_total{dlh_workflow="$workflow"}[30s])', "datasource": DS, "legendFormat": "batched"},
        ],
        "fieldConfig": {"defaults": {"unit": "Bps"}},
        "gridPos": {"x": 16, "y": 16, "w": 8, "h": 8},
    },
]

with open(PATH) as f:
    d = json.load(f)

# Shift existing y>=8 panels (Verdict-overall, SLO table) down by 16 to make room for rows 2+3.
for p in d["panels"]:
    gp = p.get("gridPos", {})
    if gp.get("y", 0) >= 8:
        gp["y"] += 16

d["panels"].extend(ROW2)
d["panels"].extend(ROW3)

with open(PATH, "w") as f:
    json.dump(d, f, indent=2)
    f.write("\n")
print(f"{PATH}: {len(d['panels'])} panels total")
```

Run it:

```bash
python3 /tmp/add-kafka-rows.py
```

Expected: `dashboards/grafana/dlh-kafka.json: 11 panels total`.

- [ ] **Step 2: Sync + lint + deploy**

```bash
make sync-dashboards
helm lint helm/dlh-test-fw
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw --timeout 3m
```

Expected: lint clean; upgrade succeeds.

- [ ] **Step 3: Run a fresh kafka scenario + verify new panel data**

```bash
make run-kafka
```

Bash timeout ≥ 600000 ms. Expected: `Final phase: Succeeded`.

```bash
wf=$(kubectl -n dlh-test-fw get workflow -o name | grep -E 'kafka-broker-partition-[0-9]{8}-[0-9]{6}$' | sort | tail -1 | sed 's|.*/||')
kubectl -n dlh-test-fw port-forward svc/dlh-victoria-metrics-single-server 8428:8428 >/tmp/pf.log 2>&1 &
PF=$!
sleep 5
start=$(kubectl -n dlh-test-fw get workflow "$wf" -o jsonpath='{.status.startedAt}')
finish=$(kubectl -n dlh-test-fw get workflow "$wf" -o jsonpath='{.status.finishedAt}')
s=$(TZ=UTC date -j -f "%Y-%m-%dT%H:%M:%SZ" "$start" +%s)
f=$(TZ=UTC date -j -f "%Y-%m-%dT%H:%M:%SZ" "$finish" +%s)
for label in vus iter-p95 batch-queue-p95 retries; do
  case "$label" in
    vus)             q="k6_vus%7Bdlh_workflow%3D%22${wf}%22%7D" ;;
    iter-p95)        q="k6_iteration_duration_p95%7Bdlh_workflow%3D%22${wf}%22%7D" ;;
    batch-queue-p95) q="k6_kafka_writer_batch_queue_seconds_p95%7Bdlh_workflow%3D%22${wf}%22%7D" ;;
    retries)         q="rate(k6_kafka_writer_retries_count_total%7Bdlh_workflow%3D%22${wf}%22%7D%5B30s%5D)" ;;
  esac
  r=$(curl -s "http://localhost:8428/api/v1/query_range?query=${q}&start=${s}&end=${f}&step=15")
  c=$(echo "$r" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['data']['result']))")
  printf "  %-16s c=%s\n" "$label" "$c"
done
kill $PF 2>/dev/null
```

Expected: at least 1 series for each. If `k6_kafka_writer_*` returns 0 series, the kafka runner may not have emitted them this run — re-check the runner's `kafka_op=produce` setting.

- [ ] **Step 4: Commit**

```bash
git add dashboards/grafana/dlh-kafka.json helm/dlh-test-fw/files/dashboards/dlh-kafka.json
git commit -m "feat(dashboards/dlh-kafka): add row 2 (generic k6) + row 3 (xk6-kafka writer internals)

Six new timeseries panels at y=8 (generic: VUs, iteration p95, throughput)
and y=16 (xk6-kafka: writer batch queue p95, retries+errors, batch
bytes/sec). Existing Verdict-overall + SLO table shifted to y=24/29.
Total panels: 5 -> 11."
```

---

## Task 8: dlh-doris — add row 2 (VUs, Stream Load latency percentiles, Data throughput)

**Files:**
- Modify: `dashboards/grafana/dlh-doris.json`
- Modify (synced): `helm/dlh-test-fw/files/dashboards/dlh-doris.json`

Note: Doris is NO-GO; no live verification. JSON shape matches the mysql pattern, just substituting Doris metric names. Helm lint + kubeconform are the validation here.

- [ ] **Step 1: Inject 3 new panels via Python**

Save as `/tmp/add-doris-row2.py`:

```python
import json

PATH = "dashboards/grafana/dlh-doris.json"
DS = {"type": "prometheus", "uid": "VictoriaMetrics"}

PANELS = [
    {
        "type": "timeseries",
        "title": "VUs over time",
        "description": "k6 virtual user count vs configured max.",
        "targets": [
            {"expr": 'k6_vus{dlh_workflow="$workflow"}',     "datasource": DS, "legendFormat": "current"},
            {"expr": 'k6_vus_max{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "max"},
        ],
        "gridPos": {"x": 0, "y": 8, "w": 8, "h": 8},
    },
    {
        "type": "timeseries",
        "title": "Stream Load latency percentiles",
        "description": "Doris Stream Load duration distribution from the doris.js runner.",
        "targets": [
            {"expr": 'k6_dlh_doris_streamload_duration_seconds_avg{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "avg"},
            {"expr": 'k6_dlh_doris_streamload_duration_seconds_p95{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "p95"},
            {"expr": 'k6_dlh_doris_streamload_duration_seconds_p99{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "p99"},
            {"expr": 'k6_dlh_doris_streamload_duration_seconds_max{dlh_workflow="$workflow"}', "datasource": DS, "legendFormat": "max"},
        ],
        "fieldConfig": {"defaults": {"unit": "s"}},
        "gridPos": {"x": 8, "y": 8, "w": 8, "h": 8},
    },
    {
        "type": "timeseries",
        "title": "Data throughput",
        "description": "k6 total bytes sent/received per second on the wire.",
        "targets": [
            {"expr": 'rate(k6_data_sent_total{dlh_workflow="$workflow"}[30s])',     "datasource": DS, "legendFormat": "sent"},
            {"expr": 'rate(k6_data_received_total{dlh_workflow="$workflow"}[30s])', "datasource": DS, "legendFormat": "received"},
        ],
        "fieldConfig": {"defaults": {"unit": "Bps"}},
        "gridPos": {"x": 16, "y": 8, "w": 8, "h": 8},
    },
]

with open(PATH) as f:
    d = json.load(f)

# Shift existing y>=8 panels (Verdict-overall, SLO table) down by 8.
for p in d["panels"]:
    gp = p.get("gridPos", {})
    if gp.get("y", 0) >= 8:
        gp["y"] += 8

d["panels"].extend(PANELS)

with open(PATH, "w") as f:
    json.dump(d, f, indent=2)
    f.write("\n")
print(f"{PATH}: {len(d['panels'])} panels total")
```

Run it:

```bash
python3 /tmp/add-doris-row2.py
```

Expected: `dashboards/grafana/dlh-doris.json: 8 panels total`.

- [ ] **Step 2: Sync + lint**

```bash
make sync-dashboards
helm lint helm/dlh-test-fw
helm upgrade dlh helm/dlh-test-fw -n dlh-test-fw --timeout 3m
```

Expected: lint clean; upgrade succeeds.

- [ ] **Step 3: kubeconform smoke (CI parity)**

```bash
helm template dlh helm/dlh-test-fw > /tmp/rendered.yaml
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  /tmp/rendered.yaml
```

Expected: `Invalid: 0, Errors: 0`. (Dashboard JSONs ride inside a ConfigMap, so kubeconform validates the CM shape — not the JSON contents.)

- [ ] **Step 4: Commit**

```bash
git add dashboards/grafana/dlh-doris.json helm/dlh-test-fw/files/dashboards/dlh-doris.json
git commit -m "feat(dashboards/dlh-doris): add row 2 — VUs, Stream Load percentiles, throughput

Three new timeseries panels matching the dlh-mysql/dlh-kafka pattern, with
Doris-specific Stream Load latency metrics. Not live-tested (Doris target
is NO-GO); JSON shape verified by helm lint + kubeconform.
Total panels: 5 -> 8."
```

---

## Task 9: FINDINGS + final suite + merge + push + cleanup + tag

**Files:**
- Modify: `docs/FINDINGS.md` — append Plan 13 section

- [ ] **Step 1: Append Plan 13 section to `docs/FINDINGS.md`**

Read the FINDINGS.md tail to match heading style. Append:

```markdown
## Plan 13 — Per-target dashboard enrichment + chaos overlay (2026-05-20)

- Two new gauges from verdict-job:
  `dlh_chaos_window_start_unixtime{dlh_workflow=...,dlh_scenario=...}` and
  `dlh_chaos_window_end_unixtime{...}`. Values are unix-epoch seconds of the
  chaos window's bounds. Source: `eval.Result.ChaosWindowStart/End`,
  derived in `Evaluate` from `window.Params.LoadStart + ChaosStartAfter`
  (and + ChaosDuration). Pushed via the existing VM text-import endpoint
  in `metrics.build()` — no new client, no signature change to
  `metrics.Push`.
- Each per-run dashboard (`dlh-mysql`, `dlh-kafka`, `dlh-doris`,
  `dlh-run-detail`) gains an `annotations.list` block with two Prometheus
  annotation entries using `useValueForTime: true`. Grafana renders
  vertical marks AT the gauge's value-as-timestamp (not at the metric's
  sample timestamp). Orange = chaos start; green = chaos recovered.
  Marks appear automatically on every timeseries panel of the dashboard.
- Panel additions are pure JSON edits driven by small Python scripts
  (`/tmp/add-mysql-row2.py`, `/tmp/add-kafka-rows.py`,
  `/tmp/add-doris-row2.py`). The scripts shift existing y>=8 panels down
  by 8 (mysql/doris) or 16 (kafka) to insert new rows at y=8 (and y=16
  for kafka's row 3) without breaking gridPos arithmetic.
- New panel queries are all `{dlh_workflow="$workflow"}`-filtered to
  match the per-run dashboard semantic. Latency-percentile panels overlay
  the avg/p95/p99/max siblings of the existing `*_p95` series — they're
  already pushed by k6, just unused.
- Pitfall: `useValueForTime` is documented since Grafana 9.x. Chart
  `grafana 8.15.0` bundles Grafana v11.x — compatible. If a future chart
  downgrade drops Grafana below v9, annotations will plot at the metric's
  sample timestamp (i.e. end-of-run) rather than at the chaos window, and
  the visual cue will be wrong.
- Pitfall: xk6-kafka writer metrics (`k6_kafka_writer_*`) exist only when
  the runner does `kafka_op=produce`. The kafka panel-row-3 panels go
  empty if a future scenario sets `kafka_op=consume` (which would emit
  `_reader_*` metrics instead). Each panel's description text notes this.
- Pitfall: k6 prom-rw push interval is sparse — `rate(...[30s])` over a
  60s chaos window may have 1-3 samples. Panels still render but lines
  look chunky. Acceptable; same gotcha existed before Plan 13.
```

- [ ] **Step 2: Final suite re-run**

```bash
make run-mysql
make run-kafka
./scripts/verify-templates.sh
cd verdict-job && go test ./... && cd -
```

Sequential. Bash timeout ≥ 900000 ms per `make`. Expected:
- mysql `Final phase: Succeeded`
- kafka `Final phase: Succeeded`
- verify-templates `PASS: all 10 WorkflowTemplates present`
- go test: all 6 packages PASS

- [ ] **Step 3: Commit FINDINGS**

```bash
git add docs/FINDINGS.md
git commit -m "docs(findings): record Plan 13 dashboard enrichment + chaos overlay"
```

- [ ] **Step 4: Merge to main with --no-ff**

From the **main worktree**:

```bash
cd /Users/allen/repo/dlh-test-fw
git status            # should be clean (or only an untracked plan doc)
git checkout main
git pull origin main  # in case main moved
git merge --no-ff feat/plan13-dashboard-enrichment -m "$(cat <<'EOF'
Merge feat/plan13-dashboard-enrichment: more per-target panels + chaos overlay

Plan 13 fills two gaps that Plan 8 (per-type dashboards) left open and
Plan 12 (Chaos Mesh migration) widened (deleted dlh_verdict_chaos_pass).

- verdict-job emits 2 new gauges at end-of-run: dlh_chaos_window_*_unixtime
  (unix-epoch seconds; labels dlh_workflow + dlh_scenario). Source:
  eval.Result.ChaosWindow{Start,End}, set in Evaluate from window.Params.
- All 4 per-run dashboards (mysql, kafka, doris, run-detail) gain a
  Grafana annotations.list block with useValueForTime:true — orange/green
  vertical marks at chaos start/recovery on every timeseries panel.
- dlh-mysql: +3 panels (VUs, latency percentiles, data throughput).
- dlh-kafka: +6 panels (3 generic + 3 xk6-kafka writer internals:
  batch queue p95, retries+errors, batch bytes/sec).
- dlh-doris: +3 panels (matching mysql shape with Doris metrics; not
  live-tested per NO-GO target).

Total panel count: mysql 5->8, kafka 5->11, doris 5->8, run-detail 5
(unchanged; annotations only).

Spec: docs/superpowers/specs/2026-05-20-dashboard-enrichment-design.md
Plan: docs/superpowers/plans/2026-05-20-01-dashboard-enrichment.md
EOF
)"
git log --first-parent --oneline -6
```

- [ ] **Step 5: Push + watch CI**

```bash
git push origin main
sleep 10
gh run list --branch main --limit 2
```

Expected: push succeeds; main CI is `in_progress` or `success`.

- [ ] **Step 6: Cleanup worktree + branch**

```bash
git worktree remove ../dlh-test-fw-plan13
git push origin --delete feat/plan13-dashboard-enrichment 2>/dev/null || true
git branch -d feat/plan13-dashboard-enrichment
git worktree list
```

Expected: only main worktree; branch deleted.

- [ ] **Step 7: Tag + push tag**

```bash
git tag plan13-dashboard-enrichment
git push origin plan13-dashboard-enrichment
git log --first-parent --oneline -10
```

---

## Self-Review notes (author check, fresh-eyes pass)

- **Spec coverage:**
  - Goal 1 (dlh-mysql 3 panels): Task 6.
  - Goal 2 (dlh-kafka 6 panels): Task 7.
  - Goal 3 (dlh-doris 3 panels): Task 8.
  - Goal 4 (chaos overlay gauges + annotations): Tasks 2 (Result fields), 3 (gauge emission), 4 (live verification), 5 (annotation source on all 4 dashboards).
- **Spec testing matrix** rows mapped: gauges emitted → Task 4 Step 3; Go tests → Task 2 Step 7, Task 3 Step 5, Task 9 Step 2; each new panel returns data → Task 6 Step 3, Task 7 Step 3, Task 8 (lint-only for Doris); useValueForTime works → Task 5 Step 5 (human visual); existing panels unaffected → Task 9 Step 2; helm lint + kubeconform → Tasks 5/6/7/8 each include lint, Task 8 adds explicit kubeconform; make run-mysql + make run-kafka → Task 4 + Task 9.
- **Spec success criteria** 1-9: all verified inside Tasks 2-9. Final main-CI green check is Task 9 Step 5.
- **Spec risks** mitigations:
  - Grafana version compat → Task 1 Step 3.
  - `enable: true` / annotation rendering → Task 5 Step 5 (visual check + browser devtools fallback).
  - Sparse k6 prom-rw → accepted; not a new pitfall; FINDINGS note in Task 9.
  - xk6-kafka writer empty on consume → panel description text in Task 7 Step 1.
  - Doris blind → Task 8 explicitly accepts; kubeconform pass is the proxy.
- **Placeholder scan**: no TBD/TODO/etc. Conditional branches ("if image isn't loaded, re-tag" in Task 4 Step 1) are explicit and bounded.
- **Type consistency**:
  - `ChaosWindowStart` / `ChaosWindowEnd` used identically in Task 2 (eval), Task 3 (metrics), and Task 9 (FINDINGS reference).
  - Gauge names `dlh_chaos_window_start_unixtime` / `dlh_chaos_window_end_unixtime` consistent across Tasks 3, 4, 5, 9.
  - Panel count totals: mysql 5→8, kafka 5→11, doris 5→8, run-detail 5 (unchanged). Same totals appear in spec File summary, plan File Structure, Task 9 merge commit body, and FINDINGS.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-20-01-dashboard-enrichment.md`. Two execution options:

**1. Subagent-Driven (recommended)** — fresh subagent per task. Tasks 4, 6, 7 each run a live scenario (~5 min cluster wait); subagents handle politely in background.

**2. Inline Execution** — batch with checkpoints; terminal sits on cluster waits.

Which approach?
