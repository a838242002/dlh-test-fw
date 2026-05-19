# Per-Target Dashboard Enrichment + Chaos Overlay — Design Spec

**Date**: 2026-05-20
**Status**: Draft, awaiting user review
**Project**: dlh-test-fw
**Builds on**: Plan 8 (per-type dashboards), Plan 12 (Chaos Mesh migration).

## Why

After Plan 8 + Plan 12, each per-type dashboard (`dlh-mysql`, `dlh-kafka`, `dlh-doris`) has only 5 panels: 3 protocol-specific timeseries + a Verdict-overall stat + a SLO-thresholds table. Two gaps:

1. **Available metrics are unused.** VM holds rich k6 data we never plot — VUs over time, iteration-level latency percentiles, data sent/received bytes, and for kafka the entire `k6_kafka_writer_*` family (batch queue time, retry counts, writer error rate, batch bytes per second). When a scenario fails, an operator opening the dashboard has no way to see "how many VUs were ramped up?" or "did the kafka writer's batch queue clog up before errors started?" — they have to drop to PromQL.
2. **No chaos timeline reference.** Timeseries panels show k6 metrics over time, but there's no visual marker for *when chaos was injected* and *when it recovered*. Operators have to mentally calculate from scenario parameters or stare at the verdict-eval window definition. Chaos overlays are standard practice on chaos-engineering dashboards.

This plan fills both gaps using metrics already in VM (no new scrapers, no exporters).

## Goals (in scope)

1. **dlh-mysql**: add 3 new panels — VUs over time, Query latency percentiles (avg/p95/p99/max overlaid), Data throughput (sent + received bytes/sec).
2. **dlh-kafka**: add 6 new panels — the same 3 generic ones (with kafka's iteration p95 in place of mysql's query p95) plus 3 xk6-kafka writer internals (`batch queue p95`, `retries + errors`, `batch bytes/sec`).
3. **dlh-doris**: add 3 new panels — same generic trio as mysql, with Stream Load p95 in place of Query p95 where relevant. (Dashboard is not live-tested — Doris target is NO-GO — but the JSON ships and renders.)
4. **Chaos overlay**: emit two new gauges from verdict-job (`dlh_chaos_window_start_unixtime`, `dlh_chaos_window_end_unixtime`) at end-of-run, labelled by `dlh_workflow` + `dlh_scenario`. Add a Grafana annotations source to all 4 per-run dashboards (mysql + kafka + doris + dlh-run-detail) that uses `useValueForTime: true` to render orange/green vertical marks AT the chaos start and recovery timestamps.

## Goals (out of scope, deferred)

- **Target-side scraping** (mysql_exporter, kafka JMX exporter, doris FE metrics, kube-state-metrics, chaos-mesh-controller metrics). Requires deploying exporters + ServiceMonitors + adjusting VM scrape config. Substantial separate plan. Document as future Phase 4 work.
- **dlh-history dashboard changes** — history is cross-run aggregation; chaos overlay doesn't apply (different runs have different chaos windows). The Plan-12 cleanup is enough.
- **A unified "single dashboard per scenario type with a variable to switch target"** — interesting refactor but not part of this enrichment.
- **Latency heatmaps** instead of multi-percentile line overlays. k6's exported `_p95`/`_p99` gauges aren't bucketed; heatmap would require histogram metrics k6 doesn't emit.
- **Alert rules / threshold lines on panels** (e.g. red guideline at SLO `lt` value). Could be useful but adds template-variable plumbing; defer.

## Architecture

```
verdict-job/internal/metrics/metrics.go     MODIFIED  +2 new Prometheus gauges
verdict-job/cmd/verdict/main.go             MODIFIED  pass chaos window timestamps into metrics.Emit
verdict-job/internal/metrics/metrics_test.go MODIFIED expand test fixture to cover new gauges

dashboards/grafana/
├── dlh-mysql.json                          MODIFIED  +3 panels (row 2) + annotations source
├── dlh-kafka.json                          MODIFIED  +6 panels (rows 2+3) + annotations source
├── dlh-doris.json                          MODIFIED  +3 panels (row 2) + annotations source
└── dlh-run-detail.json                     MODIFIED  +annotations source (no new panels — row 1 is already generic)

helm/dlh-test-fw/files/dashboards/          synced via `make sync-dashboards`

docs/FINDINGS.md                            APPENDED  Plan 13 section
```

Net: 5 source files + 4 chart-embedded copies = 9 modified files. Zero new files. ~30 LOC in Go.

## Per-target panel additions

### dlh-mysql — new row 2 (y=8, three panels of w=8 each)

| Panel | PromQL | Notes |
|---|---|---|
| `VUs over time` | `k6_vus{dlh_workflow="$workflow"}` + `k6_vus_max{dlh_workflow="$workflow"}` as overlay | Shows load ramp profile. legendFormat: `current` / `max`. |
| `Query latency percentiles` | `k6_dlh_mysql_query_duration_seconds_avg{...}`, `_p95`, `_p99`, `_max` — 4 series overlaid | Reveals tail-latency divergence. unit: `s`. |
| `Data throughput` | `rate(k6_data_sent_total{dlh_workflow="$workflow"}[30s])` and `rate(k6_data_received_total[30s])` | unit: `Bps`. Stacked or overlaid. |

Existing panels (row 1, y=0-7) and verdict panels (y=21+) unchanged.

### dlh-kafka — new row 2 (y=8, generic k6) + row 3 (y=16, xk6-kafka writer internals)

Row 2 (3 panels, w=8 each):

| Panel | PromQL |
|---|---|
| `VUs over time` | `k6_vus{dlh_workflow="$workflow"}`, `k6_vus_max{dlh_workflow="$workflow"}` |
| `Iteration p95` | `k6_iteration_duration_p95{dlh_workflow="$workflow"}` + `_p99` overlay, unit `ms` |
| `Data throughput` | `rate(k6_data_sent_total[30s])`, `rate(k6_data_received_total[30s])`, unit `Bps` |

Row 3 (3 panels, w=8 each):

| Panel | PromQL |
|---|---|
| `Writer batch queue p95` | `k6_kafka_writer_batch_queue_seconds_p95{dlh_workflow="$workflow"}` + `_p99` overlay, unit `s` |
| `Writer retries + errors` | `rate(k6_kafka_writer_retries_count_total{dlh_workflow="$workflow"}[30s])`, `rate(k6_kafka_writer_error_count_total[30s])` overlay |
| `Writer batch bytes/sec` | `rate(k6_kafka_writer_batch_bytes_total{dlh_workflow="$workflow"}[30s])`, unit `Bps` |

Existing row 1 (Produce rate / Produce p95 / Errors) and verdict panels unchanged. Row 1 stays at y=0-7; new rows at y=8-15 and y=16-23; verdict rows shift down accordingly (overall to y=24, SLO table to y=29).

### dlh-doris — new row 2 (y=8, three panels of w=8 each)

| Panel | PromQL |
|---|---|
| `VUs over time` | `k6_vus{dlh_workflow="$workflow"}`, `k6_vus_max{dlh_workflow="$workflow"}` |
| `Stream Load latency percentiles` | `k6_dlh_doris_streamload_duration_seconds_avg`, `_p95`, `_p99`, `_max` overlaid |
| `Data throughput` | `rate(k6_data_sent_total[30s])`, `rate(k6_data_received_total[30s])`, unit `Bps` |

Not live-tested (Doris NO-GO) but JSON ships matching the new pattern.

## Chaos overlay mechanism

### Verdict-job changes

`verdict-job/internal/eval/eval.go` already computes `chaosWindowStart` and `chaosWindowEnd` from `load_start_ts + chaos_start_after` and `+ chaos_duration` to drive PromQL windowing in SLO eval. The numbers are right there; they just don't get out of the package.

**Code change**:

1. `eval.Run` (or its result struct) exposes `ChaosWindowStart time.Time` and `ChaosWindowEnd time.Time`.
2. `cmd/verdict/main.go` extracts those two from the eval result and passes them to `metrics.Emit(...)`.
3. `internal/metrics/metrics.go` adds two new gauges:

```go
gaugeChaosWindowStart := prometheus.NewGauge(prometheus.GaugeOpts{
    Name: "dlh_chaos_window_start_unixtime",
    Help: "Unix-epoch second at which the chaos window started for this workflow run.",
    ConstLabels: prometheus.Labels{
        "dlh_workflow": workflowName,
        "dlh_scenario": scenarioLabel,
    },
})
gaugeChaosWindowStart.Set(float64(chaosWindowStart.Unix()))
// Similarly for dlh_chaos_window_end_unixtime.
```

Both are pushed via remote-write to VM at end-of-run, same path as `dlh_verdict_overall`.

4. `internal/metrics/metrics_test.go` gains assertions on the two new gauge families.

LOC budget: ~25 in Go production + ~30 in tests.

### Grafana annotations source

Each of the 4 per-run dashboards (`dlh-mysql`, `dlh-kafka`, `dlh-doris`, `dlh-run-detail`) gains an `annotations.list` block in its JSON:

```json
"annotations": {
  "list": [
    {
      "name": "chaos start",
      "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" },
      "expr": "dlh_chaos_window_start_unixtime{dlh_workflow=\"$workflow\"}",
      "useValueForTime": true,
      "iconColor": "orange",
      "tagKeys": "",
      "titleFormat": "chaos start",
      "enable": true
    },
    {
      "name": "chaos recovered",
      "datasource": { "type": "prometheus", "uid": "VictoriaMetrics" },
      "expr": "dlh_chaos_window_end_unixtime{dlh_workflow=\"$workflow\"}",
      "useValueForTime": true,
      "iconColor": "green",
      "tagKeys": "",
      "titleFormat": "chaos recovered",
      "enable": true
    }
  ]
}
```

**Why `useValueForTime: true`** — without it, Grafana plots the annotation at the metric SAMPLE's timestamp (i.e. end-of-run when verdict-job pushed). With it set, Grafana uses the VALUE of the gauge AS the timestamp. Since we publish the chaos start/end as unix epochs, Grafana renders the marks AT those exact moments. Documented behaviour since Grafana 9.x; chart 8.15.0 bundles Grafana v11.x — compatible.

The marks are vertical lines (with the chosen icon colour) drawn across every timeseries panel on the dashboard automatically. No per-panel configuration needed.

## Code-level details — verdict-job

### Current eval pipeline (`internal/eval/eval.go`)

```go
type Result struct {
    Overall    bool
    Thresholds []ThresholdResult
    // (no ChaosVerdict — Plan 12 removed it)
}

func Evaluate(slo SLO, prom prom.Client, loadStart time.Time, chaosStartAfter, chaosDuration, loadDuration time.Duration, scenarioLabel string) (Result, error) {
    chaosStart := loadStart.Add(chaosStartAfter)
    chaosEnd := chaosStart.Add(chaosDuration)
    // ... thresholds evaluated against chaos / recovery windows ...
}
```

### New result + signature

```go
type Result struct {
    Overall          bool
    Thresholds       []ThresholdResult
    ChaosWindowStart time.Time   // NEW
    ChaosWindowEnd   time.Time   // NEW
}

// Inside Evaluate, after computing chaosStart / chaosEnd:
return Result{
    Overall:          allPassed,
    Thresholds:       thresholdResults,
    ChaosWindowStart: chaosStart,
    ChaosWindowEnd:   chaosEnd,
}, nil
```

### `cmd/verdict/main.go` change

Existing call: `metrics.Emit(ctx, pushURL, workflow, scenario, result.Overall, result.Thresholds)`.

New call: `metrics.Emit(ctx, pushURL, workflow, scenario, result.Overall, result.Thresholds, result.ChaosWindowStart, result.ChaosWindowEnd)`.

### `internal/metrics/metrics.go` change

Existing function signature absorbs the two extra `time.Time` arguments. Inside the function, the existing `prometheus.NewGauge` calls get joined by two new ones:

```go
func Emit(ctx context.Context, pushURL, workflow, scenario string, overall bool, thresholds []eval.ThresholdResult, chaosStart, chaosEnd time.Time) error {
    reg := prometheus.NewRegistry()
    // ... existing gauges ...

    chaosWindowStartGauge := prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "dlh_chaos_window_start_unixtime",
        Help: "Unix-epoch second at which chaos was injected for this workflow run.",
        ConstLabels: prometheus.Labels{"dlh_workflow": workflow, "dlh_scenario": scenario},
    })
    chaosWindowStartGauge.Set(float64(chaosStart.Unix()))
    reg.MustRegister(chaosWindowStartGauge)

    chaosWindowEndGauge := prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "dlh_chaos_window_end_unixtime",
        Help: "Unix-epoch second at which chaos was recovered for this workflow run.",
        ConstLabels: prometheus.Labels{"dlh_workflow": workflow, "dlh_scenario": scenario},
    })
    chaosWindowEndGauge.Set(float64(chaosEnd.Unix()))
    reg.MustRegister(chaosWindowEndGauge)

    // ... existing push.New(pushURL, ...).Gatherer(reg).Push() ...
}
```

### `internal/metrics/metrics_test.go` change

Existing tests assert the registered gauges by name. Add two cases — one verifying `dlh_chaos_window_start_unixtime` was emitted with the right value (call `Emit` with `chaosStart = time.Unix(1700000000, 0)`, then inspect the registry), and one for end.

## Testing matrix

| Element | How |
|---|---|
| New gauges emitted | After `make run-mysql`, `curl ...query=dlh_chaos_window_start_unixtime{dlh_workflow="$wf"}` returns a unix epoch matching `(workflow.startedAt + chaos_start_after).Unix()` to within ~1s |
| Go unit tests pass | `cd verdict-job && go test ./...` — all packages green, including new metrics tests |
| Each new panel returns data | `curl /api/v1/query_range?query=<expr>&start=<wf.startedAt>&end=<wf.finishedAt>&step=15` returns non-zero series within the workflow's actual time window |
| `useValueForTime: true` works | Open dlh-mysql in Grafana, pick the latest workflow; vertical orange mark appears at chaos start time, green at chaos end. Marks visible across all timeseries panels in the dashboard. |
| Annotations on dlh-run-detail | Same check on the generic Run Detail dashboard |
| Existing panels still render | After helm upgrade + a fresh `make run-mysql`, all pre-Plan-13 panels (3 in row 1, 2 verdict) display correctly |
| Helm lint + kubeconform pass | Same CI guardrails as Plan 10 |
| `make run-mysql` + `make run-kafka` Succeed | End-to-end smoke; verdict pipeline still works with the expanded `metrics.Emit` signature |

## Success criteria

1. `dlh_chaos_window_start_unixtime{dlh_workflow=...,dlh_scenario=...}` and `dlh_chaos_window_end_unixtime{...}` visible in VM after a workflow run.
2. dlh-mysql JSON has 8 panels total (3 + 3 + 2).
3. dlh-kafka JSON has 11 panels total (3 + 3 + 3 + 2).
4. dlh-doris JSON has 8 panels total.
5. dlh-run-detail JSON has 5 panels (unchanged count) + annotations source.
6. All 4 per-run dashboards display orange/green vertical chaos marks when viewing a workflow.
7. `go test ./...` in verdict-job passes; existing 6 packages + expanded metrics tests all green.
8. `make run-mysql` and `make run-kafka` both Succeed end-to-end.
9. CI on `main` (Plan 10 four-job pipeline) passes after merge.

## Risks

- **Grafana version compatibility for `useValueForTime`.** Documented since Grafana 9.x; chart `grafana 8.15.0` bundles Grafana v11.x. Verified in plan baseline before final commit.
- **`enable: true` does NOT make the annotation source on by default in some Grafana versions** — instead `hide: false` may be needed, or `enable: true` AND `default: true`. Plan baseline confirms the actual key name (Grafana JSON dashboard format quirks).
- **k6 prom-rw push interval is sparse** (single sample per metric per workflow in current setup). All the new panels use `rate(...[30s])` or instant queries — the rate panels will only have data at the cadence k6 pushes. If push interval is ~5s, `rate([30s])` over a 60s window gets ~5-10 samples — plottable but coarse. Acceptable; not a regression.
- **xk6-kafka writer metrics exist only when producing.** If a future kafka scenario sets `kafka_op=consume`, the writer panels go empty. Document this in the panel description text so a future reader doesn't think the panel is broken.
- **Doris dashboard updates are blind** — no live target to validate against. Plan accepts this risk: JSON ships with the new pattern; helm-renders correctly; kubeconform passes; first live Doris run someday will tell us if the queries are right. Mitigation: keep the Doris JSON SCHEMA-IDENTICAL to mysql (just different metric names), so syntactic correctness is high-confidence even without runtime test.
- **Annotation source can fire BEFORE the gauge metric is published** — if Grafana renders the dashboard while the workflow is still running (chaos has been applied but verdict hasn't pushed yet), the annotation source returns no data. Acceptable — chaos overlay is most useful in post-mortem, where the workflow has finished and the gauges are in VM.
- **Plan 11 `scripts/run-scenario.sh` queue probe unaffected** — annotations live in Grafana, not in workflow submission.
- **VM lookback-delta is 5 min** — chaos window gauges go stale to instant queries after 5 min. Annotation queries are range queries (the workflow time window), so this doesn't bite them; but a Grafana stat panel showing "current chaos window" would. We're not adding such a panel, so no issue.

## Relationship to other plans

- **Plan 8** (per-type dashboards): this plan extends those dashboards. No replacement.
- **Plan 9** (util WTs + slo_vars): unaffected.
- **Plan 10** (CI): no CI changes. The new JSON files pass `kubeconform` via existing skip list.
- **Plan 11** (scenario queue): unaffected.
- **Plan 12** (Chaos Mesh): the chaos-window metrics ARE the bridge between Plan 12's chaos engine and the dashboard layer. Plan 12 chose to drop `dlh_verdict_chaos_pass`; Plan 13 reintroduces a different chaos signal — but only the timing window, not a pass/fail verdict. That's intentional: Plan 12 said the chaos-applied signal is encoded in Argo step success; this plan adds a *timing* signal that's independent of pass/fail.

## File summary

| Path | Change |
|---|---|
| `verdict-job/internal/eval/eval.go` | MODIFIED — expose ChaosWindow{Start,End} on Result |
| `verdict-job/internal/metrics/metrics.go` | MODIFIED — accept + emit two new gauges |
| `verdict-job/internal/metrics/metrics_test.go` | MODIFIED — expand fixture/assertions |
| `verdict-job/cmd/verdict/main.go` | MODIFIED — pass chaos times into metrics.Emit |
| `dashboards/grafana/dlh-mysql.json` | MODIFIED — +3 panels + annotations source |
| `dashboards/grafana/dlh-kafka.json` | MODIFIED — +6 panels + annotations source |
| `dashboards/grafana/dlh-doris.json` | MODIFIED — +3 panels + annotations source |
| `dashboards/grafana/dlh-run-detail.json` | MODIFIED — +annotations source only |
| `helm/dlh-test-fw/files/dashboards/*.json` | MODIFIED via `make sync-dashboards` |
| `docs/FINDINGS.md` | APPENDED — Plan 13 section |

Total: 9 source files, 4 chart-embedded copies (synced), 0 new files, 0 deletions.
