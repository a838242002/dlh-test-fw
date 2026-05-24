# Run-detail UI Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the controlplane run-detail page (and the coupled Runs-list verdict column) read accurately and clearly — populated verdict, deterministic window-aware step timeline, working live updates, readable verdict values, and a plain-language scenario description.

**Architecture:** Backend-first (Go: deterministic step sort, list verdict enrichment from cached MinIO reports, SSE auth, scenario-description annotation) so the API carries the data; then frontend (Vitest-tested pure logic in `web/src/lib/*` + the `RunDetailPage`/`VerdictView` render changes) consuming it. No new backend dependencies — reuse the existing `minio.ReportReader`.

**Tech Stack:** Go 1.26 (controlplane, oapi-codegen strict handlers, client-go workflow informer), React + Vite + Tailwind + shadcn/ui, Vitest, `go test`.

**Spec:** `docs/superpowers/specs/2026-05-25-run-detail-ui-optimization-design.md`

**Pre-flight (run once before Task 1):**
```bash
cd /Users/allen/repo/dlh-test-fw/controlplane
go test ./... >/dev/null && echo "GO BASELINE OK"
cd web && pnpm install --frozen-lockfile >/dev/null && pnpm test run >/dev/null && echo "WEB BASELINE OK"
```
All paths below are relative to `controlplane/`. Commit messages use the repo convention; end bodies with the Co-Authored-By trailer.

---

## Task 1: Deterministic step order (backend)

**Files:**
- Modify: `internal/model/types.go` (the steps build in `RunDetailFromWorkflow`)
- Test: `internal/model/types_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/model/types_test.go` (create if absent, `package model`):
```go
func TestRunDetailFromWorkflow_StepsSortedByStart(t *testing.T) {
	base := metav1.Now().Time
	wf := &wfv1.Workflow{}
	wf.Name = "wf-x"
	wf.Status.Nodes = wfv1.Nodes{
		"c": {DisplayName: "verdict", Phase: "Succeeded", StartedAt: metav1.NewTime(base.Add(3 * time.Minute))},
		"a": {DisplayName: "prep-slo", Phase: "Succeeded", StartedAt: metav1.NewTime(base)},
		"b": {DisplayName: "load", Phase: "Succeeded", StartedAt: metav1.NewTime(base.Add(20 * time.Second))},
	}
	d := RunDetailFromWorkflow(wf)
	if d.Steps == nil || len(*d.Steps) != 3 {
		t.Fatalf("want 3 steps, got %v", d.Steps)
	}
	got := []string{(*d.Steps)[0].Name, (*d.Steps)[1].Name, (*d.Steps)[2].Name}
	want := []string{"prep-slo", "load", "verdict"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step order = %v, want %v", got, want)
		}
	}
}
```
Ensure imports include `"sort"`, `"time"`, `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"`, and the workflow types alias `wfv1` already used in `types.go`.

- [ ] **Step 2: Run it — expect FAIL (random map order)**

Run: `go test ./internal/model/ -run StepsSortedByStart -count=1 -v`
Expected: FAIL intermittently / on order mismatch.

- [ ] **Step 3: Add the sort after the steps loop**

In `RunDetailFromWorkflow`, immediately after `steps = append(steps, step)` loop closes and before `d.Steps = &steps`, insert:
```go
		sort.SliceStable(steps, func(i, j int) bool {
			si, sj := steps[i].StartedAt, steps[j].StartedAt
			switch {
			case si == nil && sj == nil:
				return steps[i].Name < steps[j].Name
			case si == nil:
				return false
			case sj == nil:
				return true
			case si.Equal(*sj):
				return steps[i].Name < steps[j].Name
			default:
				return si.Before(*sj)
			}
		})
```
Add `"sort"` to the import block if not present.

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/model/ -run StepsSortedByStart -count=5 -v`
Expected: PASS all 5 runs (stable order).

- [ ] **Step 5: Commit**
```bash
git add internal/model/types.go internal/model/types_test.go
git commit -m "fix(controlplane): sort run-detail steps by start time (was random map order)"
```

---

## Task 2: Verdict cache + Runs-list score enrichment (backend)

**Files:**
- Create: `internal/runs/verdictcache.go`
- Create: `internal/runs/verdictcache_test.go`
- Modify: `internal/api/handlers.go` (`ListRuns`)
- Modify: `internal/api/server.go` (wire the cache into `Deps`, if a field is needed)

**Context:** `model.RunFromWorkflow` never sets `Score`, so the Runs-list verdict column is always "—". We populate `Score` (`1.0`/`0.0`/`nil`) from the MinIO report's `overall`, cached per workflow (immutable once finished). `ReportReader.Read(ctx, name)` returns `map[string]any` with key `overall` (bool), or `mio.ErrReportNotFound`.

- [ ] **Step 1: Write the failing test for the cache**

`internal/runs/verdictcache_test.go` (`package runs`):
```go
package runs

import (
	"context"
	"errors"
	"testing"

	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
)

type fakeReader struct {
	calls int
	rep   map[string]any
	err   error
}

func (f *fakeReader) Read(_ context.Context, _ string) (map[string]any, error) {
	f.calls++
	return f.rep, f.err
}

func TestVerdictCache_PassCachedOnce(t *testing.T) {
	fr := &fakeReader{rep: map[string]any{"overall": true}}
	c := NewVerdictCache(fr)
	s, ok := c.Score(context.Background(), "wf-1", true) // terminal=true
	if !ok || s != 1.0 {
		t.Fatalf("want (1.0,true), got (%v,%v)", s, ok)
	}
	_, _ = c.Score(context.Background(), "wf-1", true)
	if fr.calls != 1 {
		t.Fatalf("expected 1 read (cached), got %d", fr.calls)
	}
}

func TestVerdictCache_FailMapsToZero(t *testing.T) {
	fr := &fakeReader{rep: map[string]any{"overall": false}}
	c := NewVerdictCache(fr)
	s, ok := c.Score(context.Background(), "wf-2", true)
	if !ok || s != 0.0 {
		t.Fatalf("want (0.0,true), got (%v,%v)", s, ok)
	}
}

func TestVerdictCache_NotFoundNotCached(t *testing.T) {
	fr := &fakeReader{err: mio.ErrReportNotFound}
	c := NewVerdictCache(fr)
	if _, ok := c.Score(context.Background(), "wf-3", true); ok {
		t.Fatal("want ok=false for missing report")
	}
	_, _ = c.Score(context.Background(), "wf-3", true)
	if fr.calls != 2 {
		t.Fatalf("missing report must NOT cache; want 2 reads, got %d", fr.calls)
	}
}

func TestVerdictCache_NonTerminalSkipsRead(t *testing.T) {
	fr := &fakeReader{rep: map[string]any{"overall": true}}
	c := NewVerdictCache(fr)
	if _, ok := c.Score(context.Background(), "wf-4", false); ok { // terminal=false
		t.Fatal("non-terminal runs must not be read")
	}
	if fr.calls != 0 {
		t.Fatalf("want 0 reads for non-terminal, got %d", fr.calls)
	}
}
```

- [ ] **Step 2: Run — expect FAIL (no such package symbols)**

Run: `go test ./internal/runs/ -run VerdictCache -v`
Expected: FAIL to compile (`NewVerdictCache` undefined).

- [ ] **Step 3: Implement the cache**

`internal/runs/verdictcache.go`:
```go
package runs

import (
	"context"
	"sync"

	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
)

// reportReader is the subset of minio.ReportReader the cache needs.
type reportReader interface {
	Read(ctx context.Context, workflowName string) (map[string]any, error)
}

// VerdictCache resolves a run's pass/fail score (1.0/0.0) from its immutable
// MinIO report, caching finished runs forever (the report never changes).
type VerdictCache struct {
	reader reportReader
	mu     sync.RWMutex
	scores map[string]float64
}

func NewVerdictCache(r reportReader) *VerdictCache {
	return &VerdictCache{reader: r, scores: map[string]float64{}}
}

// Score returns (score, true) where score is 1.0 (pass) or 0.0 (fail).
// Returns ok=false when the run isn't terminal yet, has no report, or errors
// (callers render "—"). Only terminal runs are read/cached.
func (c *VerdictCache) Score(ctx context.Context, workflow string, terminal bool) (float64, bool) {
	if !terminal {
		return 0, false
	}
	c.mu.RLock()
	if s, ok := c.scores[workflow]; ok {
		c.mu.RUnlock()
		return s, true
	}
	c.mu.RUnlock()

	rep, err := c.reader.Read(ctx, workflow)
	if err != nil || rep == nil {
		return 0, false // ErrReportNotFound or transport error → not cached
	}
	overall, ok := rep["overall"].(bool)
	if !ok {
		return 0, false
	}
	score := 0.0
	if overall {
		score = 1.0
	}
	c.mu.Lock()
	c.scores[workflow] = score
	c.mu.Unlock()
	return score, true
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/runs/ -run VerdictCache -count=1 -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Wire the cache into ListRuns**

In `internal/api/handlers.go`, locate the `ListRuns` loop:
```go
	items := make([]gen.Run, 0, len(wfs))
	for _, wf := range wfs {
		items = append(items, model.RunFromWorkflow(wf))
	}
```
Replace with:
```go
	items := make([]gen.Run, 0, len(wfs))
	for _, wf := range wfs {
		r := model.RunFromWorkflow(wf)
		if h.deps.Verdicts != nil {
			terminal := r.Status == gen.RunStatusSucceeded ||
				r.Status == gen.RunStatusFailed ||
				r.Status == gen.RunStatusError
			if s, ok := h.deps.Verdicts.Score(ctx, wf.Name, terminal); ok {
				r.Score = &s
			}
		}
		items = append(items, r)
	}
```
Change the `ListRuns` signature's first param from `_ context.Context` to `ctx context.Context` (it's currently ignored). Add a `Verdicts *runs.VerdictCache` field to the `Deps` struct (find it in `internal/api/server.go` or wherever `Deps` is defined) and construct it where `Deps.Reports` is constructed: `Verdicts: runs.NewVerdictCache(reportReader)` using the same `*minio.ReportReader`. Confirm `gen.RunStatusSucceeded` etc. exist (grep `RunStatus` in `internal/api/gen/types.gen.go`); if the generated constants differ, use the matching names.

- [ ] **Step 6: Run the backend build + tests**

Run: `go build ./... && go test ./internal/... -count=1`
Expected: builds; all pass.

- [ ] **Step 7: Commit**
```bash
git add internal/runs/verdictcache.go internal/runs/verdictcache_test.go internal/api/handlers.go internal/api/server.go
git commit -m "feat(controlplane): populate Runs-list verdict from cached MinIO report"
```

---

## Task 3: SSE events auth (backend)

**Files:**
- Modify: `internal/api/server.go` (auth middleware / events route mount)
- Modify: `internal/api/sse.go`
- Test: `internal/api/sse_auth_test.go`

**Context:** `GET /api/runs/{id}/events` returns 401 because `EventSource` can't send `Authorization` and the route doesn't honor the auth-disabled bypass. Fix: honor `DLH_AUTH_DISABLED`, and accept a token via `?access_token=`.

- [ ] **Step 1: Read the current auth wiring**

Run: `sed -n '1,140p' internal/api/server.go` and `sed -n '1,90p' internal/api/sse.go`. Identify where auth middleware is applied and how the `/api/runs/{id}/events` route is mounted (`r.Get("/api/runs/{id}/events", sseH.Handle)`), and how `cfg.AuthDisabled` and the bearer verifier are accessed.

- [ ] **Step 2: Write the failing test**

`internal/api/sse_auth_test.go` (`package api`) — exercises the token-extraction helper added in Step 3:
```go
package api

import (
	"net/http/httptest"
	"testing"
)

func TestBearerOrQueryToken(t *testing.T) {
	r1 := httptest.NewRequest("GET", "/api/runs/x/events", nil)
	r1.Header.Set("Authorization", "Bearer hdr-tok")
	if got := bearerOrQueryToken(r1); got != "hdr-tok" {
		t.Fatalf("header token: got %q", got)
	}
	r2 := httptest.NewRequest("GET", "/api/runs/x/events?access_token=q-tok", nil)
	if got := bearerOrQueryToken(r2); got != "q-tok" {
		t.Fatalf("query token: got %q", got)
	}
	r3 := httptest.NewRequest("GET", "/api/runs/x/events", nil)
	if got := bearerOrQueryToken(r3); got != "" {
		t.Fatalf("no token: got %q", got)
	}
}
```

- [ ] **Step 3: Add the helper + use it in the events auth path**

Add to `internal/api/sse.go` (or a small `internal/api/token.go`):
```go
import (
	"net/http"
	"strings"
)

// bearerOrQueryToken returns the request's auth token from the Authorization
// header ("Bearer <t>") or, failing that, the ?access_token= query param.
// EventSource cannot set headers, so SSE clients pass the token by query.
func bearerOrQueryToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("access_token")
}
```
Then, where the events route enforces auth: if `cfg.AuthDisabled` is true, skip token verification (serve); otherwise verify `bearerOrQueryToken(r)` with the same verifier used elsewhere, and write `http.StatusUnauthorized` only when it's empty/invalid. (Mount the events route with this guard rather than the standard header-only middleware.)

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/api/ -run BearerOrQueryToken -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Build + full api tests**

Run: `go build ./... && go test ./internal/api/ -count=1`
Expected: pass.

- [ ] **Step 6: Commit**
```bash
git add internal/api/sse.go internal/api/server.go internal/api/sse_auth_test.go
git commit -m "fix(controlplane): SSE events honor auth-disabled + ?access_token= (EventSource has no headers)"
```

---

## Task 4: Scenario description — annotation + derived fallback (backend)

**Files:**
- Create: `internal/model/description.go`
- Create: `internal/model/description_test.go`
- Modify: `internal/model/types.go` (`ScenarioFromTemplate`; set description)
- Modify: `api/openapi.yaml` (add `description` to `Scenario`)
- Modify: scenario WorkflowTemplates under `helm/dlh-test-fw/files/workflowtemplates/scenario/*.yaml` (add the annotation)

**Context:** The Scenario DTO comes from `ScenarioFromTemplate`. Add a `description`: use the WT annotation `dlh.scenario/description` if present, else derive from the template's known params (chaos/target/SLO).

- [ ] **Step 1: Write the failing test for the derive helper**

`internal/model/description_test.go` (`package model`):
```go
func TestScenarioDescription_PrefersAnnotation(t *testing.T) {
	got := ScenarioDescription(
		map[string]string{"dlh.scenario/description": "Custom text."},
		"mysql-pod-delete", "mysql", "pod-delete")
	if got != "Custom text." {
		t.Fatalf("annotation should win, got %q", got)
	}
}

func TestScenarioDescription_DerivedFallback(t *testing.T) {
	got := ScenarioDescription(nil, "mysql-pod-delete", "mysql", "pod-delete")
	want := "pod-delete chaos on a mysql target, evaluated against the pod-delete SLO."
	if got != want {
		t.Fatalf("derived = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run — expect FAIL (undefined)**

Run: `go test ./internal/model/ -run ScenarioDescription -v`
Expected: FAIL (undefined `ScenarioDescription`).

- [ ] **Step 3: Implement the helper**

`internal/model/description.go`:
```go
package model

import "fmt"

// ScenarioDescription returns the human description for a scenario: the
// dlh.scenario/description annotation if set, else a summary derived from the
// chaos type, target type, and SLO. Any field may be empty.
func ScenarioDescription(annotations map[string]string, id, targetType, slo string) string {
	if d := annotations["dlh.scenario/description"]; d != "" {
		return d
	}
	chaos := id // best-effort: scenario id encodes the chaos (e.g. mysql-pod-delete)
	switch {
	case targetType != "" && slo != "":
		return fmt.Sprintf("%s chaos on a %s target, evaluated against the %s SLO.", chaosFromID(id, targetType), targetType, slo)
	case targetType != "":
		return fmt.Sprintf("chaos scenario on a %s target.", targetType)
	default:
		return fmt.Sprintf("scenario %s.", chaos)
	}
}

// chaosFromID strips a leading "<targetType>-" so "mysql-pod-delete" → "pod-delete".
func chaosFromID(id, targetType string) string {
	if targetType != "" && len(id) > len(targetType)+1 && id[:len(targetType)+1] == targetType+"-" {
		return id[len(targetType)+1:]
	}
	return id
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/model/ -run ScenarioDescription -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Wire it into `ScenarioFromTemplate` + OpenAPI**

In `api/openapi.yaml` under the `Scenario` schema `properties`, add:
```yaml
        description: { type: string, description: "Human summary; from dlh.scenario/description annotation or derived." }
```
Run `make codegen` to regenerate. Then in `ScenarioFromTemplate` (`internal/model/types.go`), set `s.Description = ptr(ScenarioDescription(tmpl.Annotations, tmpl.Name, deriveTargetType(tmpl.Name), sloName))` — derive `targetType` with the existing `deriveTargetType`-equivalent (check `internal/links`; if not importable, inline the same mysql/kafka/doris prefix rule) and read `sloName` from the template's `slo_name` arg if present (empty otherwise). Use the codebase's existing string-pointer helper or `func ptr[T any](v T) *T { return &v }`.

- [ ] **Step 6: Add the annotation to the mysql scenario WT (curated example)**

In `helm/dlh-test-fw/files/workflowtemplates/scenario/mysql-pod-delete.yaml`, under `metadata:` add:
```yaml
  annotations:
    dlh.scenario/description: "Deletes a MySQL pod mid-load and verifies the service recovers within its SLO."
```
(Leave kafka/doris to the derived fallback to prove both paths.)

- [ ] **Step 7: Build + tests + chart lint**
```bash
go build ./... && go test ./internal/model/ -count=1
( cd .. && helm lint helm/dlh-test-fw >/dev/null && echo CHART OK )
```
Expected: pass.

- [ ] **Step 8: Commit**
```bash
git add internal/model/description.go internal/model/description_test.go internal/model/types.go api/openapi.yaml internal/api/gen ../helm/dlh-test-fw/files/workflowtemplates/scenario/mysql-pod-delete.yaml
git commit -m "feat(controlplane): scenario description (annotation + derived fallback)"
```

---

## Task 5: Verdict value formatting (frontend lib)

**Files:**
- Modify: `web/src/lib/verdict.ts` (add `formatThreshold`)
- Test: `web/src/lib/verdict.test.ts`

**Context:** Threshold `value`/`bound` render raw (`3.00e-6`, `0.2961`). Format by metric name. Work from `web/`.

- [ ] **Step 1: Write the failing test**

`web/src/lib/verdict.test.ts`:
```ts
import { describe, it, expect } from "vitest";
import { formatMetricByName } from "./verdict";

describe("formatMetricByName", () => {
  it("latency → time units", () => {
    expect(formatMetricByName("p95-latency-chaos", 3e-6)).toBe("3 µs");
    expect(formatMetricByName("p95-latency-chaos", 0.0004)).toBe("0.4 ms");
    expect(formatMetricByName("p95-latency-chaos", 2.5)).toBe("2.5 s");
  });
  it("rate/error → percent", () => {
    expect(formatMetricByName("error-rate-recovery", 0.2961)).toBe("29.6 %");
    expect(formatMetricByName("error-rate-recovery", 0.5)).toBe("50 %");
  });
  it("fallback → significant figures, no sci-notation", () => {
    expect(formatMetricByName("throughput", 1234.567)).toBe("1230");
  });
});
```

- [ ] **Step 2: Run — expect FAIL**

Run: `pnpm test run src/lib/verdict.test.ts`
Expected: FAIL (`formatMetricByName` not exported).

- [ ] **Step 3: Implement**

Append to `web/src/lib/verdict.ts`:
```ts
/** Format a metric value with units inferred from the metric name. */
export function formatMetricByName(metric: string, v: number): string {
  if (!Number.isFinite(v)) return String(v);
  const m = metric.toLowerCase();
  if (m.includes("latency") || m.includes("duration")) {
    if (v < 1e-3) return `${trim(v * 1e6)} µs`;
    if (v < 1) return `${trim(v * 1e3)} ms`;
    return `${trim(v)} s`;
  }
  if (m.includes("rate") || m.includes("error")) {
    return `${trim(v * 100)} %`;
  }
  return trim(v);
}

/** 3 significant figures, no scientific notation, trailing zeros trimmed. */
function trim(v: number): string {
  return parseFloat(v.toPrecision(3)).toString();
}
```
Also add a `formatBoundByName(metric, bound)` if the bound needs units — but the existing `parseVerdict` produces `bound` as a string (`"< 1"`). Extend `parseVerdict` to also expose the numeric comparator+value (`cmp: "<"|">"`, `boundValue: number`) so the view can format the bound with `formatMetricByName`; keep the existing `bound` string for back-compat.

```ts
// inside parseVerdict's threshold map, alongside `bound`:
//   cmp: typeof t.lt === "number" ? "<" : typeof t.gt === "number" ? ">" : "",
//   boundValue: typeof t.lt === "number" ? t.lt : typeof t.gt === "number" ? t.gt : NaN,
// and add `cmp?: string; boundValue?: number;` to ParsedThreshold, plus
//   window: typeof t.window === "string" ? t.window : "",   // "chaos" | "recovery"
// and `window?: string;` on ParsedThreshold.
```

- [ ] **Step 4: Run — expect PASS**

Run: `pnpm test run src/lib/verdict.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
cd .. && git add controlplane/web/src/lib/verdict.ts controlplane/web/src/lib/verdict.test.ts
git commit -m "feat(web): unit-aware verdict value/bound formatting + window parse"
```

---

## Task 6: VerdictView — window column + summary + formatting (frontend)

**Files:**
- Modify: `web/src/components/VerdictView.tsx`

- [ ] **Step 1: Update the banner + table**

Replace the PASS/FAIL banner block to add a summary, and the threshold `<TableRow>` block to use the new formatter, a **Window** column, and the bound formatter. In `web/src/components/VerdictView.tsx`:

Banner — append a summary span:
```tsx
        {parsed.overall ? <CheckCircle2 className="h-5 w-5" /> : <XCircle className="h-5 w-5" />}
        {parsed.overall ? "PASS" : "FAIL"}
        <span className="ml-auto text-sm font-normal opacity-80">
          {parsed.thresholds.filter((t) => t.passed).length} / {parsed.thresholds.length} thresholds met
        </span>
```

Table header — add the Window column between Bound and Result:
```tsx
              <TableHead>Bound</TableHead>
              <TableHead>Window</TableHead>
              <TableHead>Result</TableHead>
```

Table body row — use the formatters; highlight a failing row:
```tsx
            {parsed.thresholds.map((t) => (
              <TableRow key={t.metric} className={t.passed ? undefined : "bg-status-failed/5"}>
                <TableCell className="font-medium">{t.metric}</TableCell>
                <TableCell className="font-mono text-xs">{formatMetricByName(t.metric, t.value)}</TableCell>
                <TableCell className="font-mono text-xs">
                  {t.cmp ? `${t.cmp} ${formatMetricByName(t.metric, t.boundValue ?? NaN)}` : t.bound}
                </TableCell>
                <TableCell>{t.window
                  ? <span className="rounded px-2 py-0.5 text-[11px] " + (t.window === "chaos" ? "bg-amber-500/15 text-amber-400" : "bg-blue-500/15 text-blue-400")>{t.window}</span>
                  : <span className="text-muted-foreground">—</span>}</TableCell>
                <TableCell className={t.passed ? "text-status-success" : "text-status-failed font-semibold"}>
                  {t.passed ? "pass" : "fail"}
                </TableCell>
              </TableRow>
            ))}
```
Update the import: `import { parseVerdict, formatMetricByName } from "@/lib/verdict";` and drop the now-unused `formatMetricValue` import if it's no longer referenced. (The `className` string concat above must be written with a template literal / `cn()` — use `cn("rounded px-2 py-0.5 text-[11px]", t.window === "chaos" ? "bg-amber-500/15 text-amber-400" : "bg-blue-500/15 text-blue-400")`.)

- [ ] **Step 2: Build the web app**

Run: `pnpm build`
Expected: type-checks + builds clean.

- [ ] **Step 3: Commit**
```bash
cd .. && git add controlplane/web/src/components/VerdictView.tsx
git commit -m "feat(web): verdict table — window column, summary, unit-formatted values"
```

---

## Task 7: Steps timeline math (frontend lib)

**Files:**
- Modify: `web/src/lib/steps.ts` (add `timelineLayout`)
- Test: `web/src/lib/steps.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `web/src/lib/steps.test.ts` (create if absent):
```ts
import { describe, it, expect } from "vitest";
import { timelineLayout } from "./steps";

const steps = [
  { name: "prep", startedAt: "2026-01-01T00:00:00Z", finishedAt: "2026-01-01T00:00:30Z" },
  { name: "load", startedAt: "2026-01-01T00:00:30Z", finishedAt: "2026-01-01T00:04:16Z" },
];

describe("timelineLayout", () => {
  it("maps offset/width as % of the run window", () => {
    const lay = timelineLayout(steps, undefined);
    expect(lay.windowMs).toBe(256000);
    expect(lay.bars[0].offsetPct).toBeCloseTo(0, 3);
    expect(lay.bars[1].offsetPct).toBeCloseTo((30 / 256) * 100, 1);
    expect(lay.bars[1].widthPct).toBeCloseTo((226 / 256) * 100, 1);
  });
  it("applies a minimum visible width", () => {
    const lay = timelineLayout([{ name: "x", startedAt: "2026-01-01T00:00:00Z", finishedAt: "2026-01-01T00:00:00Z" }], undefined);
    expect(lay.bars[0].widthPct).toBeGreaterThanOrEqual(0.7);
  });
});
```

- [ ] **Step 2: Run — expect FAIL**

Run: `pnpm test run src/lib/steps.test.ts`
Expected: FAIL (`timelineLayout` not exported).

- [ ] **Step 3: Implement**

Append to `web/src/lib/steps.ts`:
```ts
export interface TimelineStep { name: string; startedAt?: string; finishedAt?: string }
export interface TimelineBar { name: string; offsetPct: number; widthPct: number; running: boolean }
export interface TimelineLayout { windowMs: number; startMs: number; bars: TimelineBar[] }

const MIN_VISIBLE_PCT = 0.7;

/** Compute bar offset/width (% of the run window) for a chronological timeline.
 *  `nowIso` (default: Date.now) bounds still-running steps. */
export function timelineLayout(steps: TimelineStep[], nowIso?: string): TimelineLayout {
  const now = nowIso ? Date.parse(nowIso) : Date.now();
  const starts = steps.map((s) => (s.startedAt ? Date.parse(s.startedAt) : now));
  const ends = steps.map((s) => (s.finishedAt ? Date.parse(s.finishedAt) : now));
  const startMs = Math.min(...starts, now);
  const endMs = Math.max(...ends, startMs + 1);
  const windowMs = Math.max(endMs - startMs, 1);
  const bars = steps.map((s, i) => {
    const offsetPct = ((starts[i] - startMs) / windowMs) * 100;
    const rawWidth = ((ends[i] - starts[i]) / windowMs) * 100;
    return { name: s.name, offsetPct, widthPct: Math.max(rawWidth, MIN_VISIBLE_PCT), running: !s.finishedAt };
  });
  return { windowMs, startMs, bars };
}

/** Map a verdict window (epoch ms) to offset/width % within the run window. */
export function windowBand(startMs: number, windowMs: number, fromMs: number, toMs: number) {
  return { offsetPct: ((fromMs - startMs) / windowMs) * 100, widthPct: ((toMs - fromMs) / windowMs) * 100 };
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `pnpm test run src/lib/steps.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
cd .. && git add controlplane/web/src/lib/steps.ts controlplane/web/src/lib/steps.test.ts
git commit -m "feat(web): timeline layout math for run-detail steps + verdict windows"
```

---

## Task 8: RunDetailPage — header, description, meta strip, timeline, states (frontend)

**Files:**
- Modify: `web/src/pages/RunDetailPage.tsx`

**Context:** This is the main view change. Implement against the agreed prototype design. `RunDetail` now carries `scenarioDescription`? — no; the description lives on the Scenario DTO (Task 4). For run-detail, fetch it: the page already has `run.scenario`; call `GET /api/scenarios/{id}` to get `description`, or (simpler) include `description` on `RunDetail` too. **Decision for this task:** add `description` to `RunDetail` in `api/openapi.yaml` (regen) and set it in `RunDetailFromWorkflow` via the same `ScenarioDescription(...)` call so the page needs one fetch. (If the WT annotations aren't on the Workflow CR, fall back to the derived form from the scenario id + target.)

- [ ] **Step 1: Add `description` to RunDetail (backend, supports this task)**

In `api/openapi.yaml` `RunDetail` properties add `description: { type: string }`; `make codegen`. In `RunDetailFromWorkflow`, set `d.Description = ptr(ScenarioDescription(wf.Annotations, d.Scenario, deriveTargetType(d.Scenario), ""))`. Build: `go build ./...`.

- [ ] **Step 2: Header — scenario name title + run-id subtitle + description**

In `RunDetailPage.tsx`, replace the title `<h1>` line and add subtitle + description. Replace:
```tsx
        <h1 className="font-mono text-lg font-semibold">{run.id}</h1>
        <StatusBadge status={status} />
```
with:
```tsx
        <h1 className="text-lg font-semibold">{run.scenario}</h1>
        <StatusBadge status={status} />
```
And after the title-row `</div>` (before the meta strip) add:
```tsx
      <div className="ml-11 -mt-2 font-mono text-xs text-muted-foreground">{run.id}</div>
      {run.description && <p className="ml-11 max-w-2xl text-sm text-muted-foreground">{run.description}</p>}
```

- [ ] **Step 3: Meta strip — de-dupe, Chaos·SLO, Priority slot**

Replace the meta strip block (the `<div className="flex flex-wrap gap-x-10 ...">`) with:
```tsx
      <div className="flex flex-wrap gap-x-10 gap-y-3 rounded-lg border bg-card px-5 py-4">
        <Meta label="Target" value={run.target || "local"} />
        <Meta label="Chaos · SLO" value={deriveCategory(run.scenario) === "scenario" ? (run.scenario.split("-").slice(1).join("-") || "—") : "—"} />
        <Meta label="Priority" value={run.priority != null ? String(run.priority) : "—"} />
        <Meta label="Started" value={relativeTime(run.startedAt)} title={new Date(run.startedAt).toLocaleString()} />
        <Meta label="Duration" value={formatDuration(run.startedAt, run.finishedAt)} />
        <Meta label="Triggered by">
          {run.triggeredBy?.id ? (
            <Link to="/schedules" className="text-primary hover:underline">{run.triggeredBy.id}</Link>
          ) : (<span className="text-muted-foreground">manual</span>)}
        </Meta>
      </div>
```
(`run.priority` is optional and populated by the priority spec; until then it renders "—". If the generated type lacks `priority`, this task may read it as `(run as any).priority` — or skip the Priority cell and leave a TODO referencing the priority spec. Prefer adding `priority?: number` to `RunDetail` in openapi now so the cell is type-clean.)

- [ ] **Step 4: Steps → timeline with verdict-windows lane**

Replace the entire Steps `<Card>` block with a timeline. Add imports at top:
```tsx
import { namedSteps, timelineLayout, windowBand } from "@/lib/steps";
import { parseVerdict } from "@/lib/verdict";
```
Replace the steps card body. Compute layout from `visibleSteps`, and windows from the verdict's `chaos_window_start/end` (+ a recovery band from the recovery threshold's `window_start/end` if present):
```tsx
      {visibleSteps.length > 0 && (() => {
        const lay = timelineLayout(visibleSteps, run.finishedAt ?? undefined);
        const v = run.verdict as Record<string, any> | undefined;
        const band = (from?: string, to?: string) =>
          from && to ? windowBand(lay.startMs, lay.windowMs, Date.parse(from), Date.parse(to)) : null;
        const chaos = band(v?.chaos_window_start, v?.chaos_window_end);
        const kindOf = (name: string) =>
          name.includes("chaos") ? "bg-amber-500" :
          name.startsWith("load") || name.includes("testrun") ? "bg-blue-500" :
          name === "verdict" ? "bg-indigo-500" : "bg-slate-600";
        return (
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <CardTitle className="text-base">Steps</CardTitle>
              <span className="text-xs text-muted-foreground">{visibleSteps.length} steps · chronological</span>
            </CardHeader>
            <CardContent>
              {chaos && (
                <div className="relative mb-2 ml-[200px] h-5">
                  <div className="absolute inset-y-0 rounded bg-amber-500/15 border-x border-dashed border-amber-500/50 text-[10px] text-amber-400 px-1"
                       style={{ left: `${chaos.offsetPct}%`, width: `${chaos.widthPct}%` }}>chaos window</div>
                </div>
              )}
              <div className="space-y-1.5">
                {visibleSteps.map((s, i) => (
                  <div key={i} className="grid grid-cols-[180px_64px_1fr] items-center gap-3">
                    <span className="flex items-center gap-2 text-sm font-medium"><StepIcon phase={s.phase} />{s.name}</span>
                    <span className="font-mono text-xs text-muted-foreground">{formatDuration(s.startedAt, s.finishedAt)}</span>
                    <span className="relative h-3.5 rounded bg-muted">
                      <span className={`absolute top-0 h-3.5 rounded ${kindOf(s.name)} ${lay.bars[i].running ? "animate-pulse" : ""}`}
                            style={{ left: `${lay.bars[i].offsetPct}%`, width: `${lay.bars[i].widthPct}%` }} />
                    </span>
                  </div>
                ))}
              </div>
              {visibleSteps.some((s) => s.message) && (
                <div className="mt-3 space-y-1">
                  {visibleSteps.filter((s) => s.message).map((s, i) => (
                    <div key={i} className="rounded border border-status-failed/40 bg-status-failed/10 px-2.5 py-1.5 font-mono text-xs text-status-failed">
                      {s.name}: {s.message}
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        );
      })()}
```
(Keep `StepIcon`, `namedSteps`, the loading/error states unchanged. The recovery band can be added the same way as `chaos` once the report's recovery window field name is confirmed against `report.json`; if not present, omit it — chaos window is the priority.)

- [ ] **Step 5: SSE — pass the token via query**

Update the `EventSource` line so authenticated deployments work. Replace:
```tsx
    const es = new EventSource(`/api/runs/${id}/events`);
```
with:
```tsx
    const tok = getAuthToken(); // existing helper that returns the SPA's bearer (empty when auth disabled)
    const es = new EventSource(`/api/runs/${id}/events${tok ? `?access_token=${encodeURIComponent(tok)}` : ""}`);
```
Find the token accessor used by `src/api/client.ts` (the `setAuthToken` counterpart); if none is exported, export a `getAuthToken()` from `src/api/client.ts` returning the stored token (or `""`).

- [ ] **Step 6: Build**

Run: `cd web && pnpm build`
Expected: clean type-check + build.

- [ ] **Step 7: Commit**
```bash
cd .. && git add controlplane/api/openapi.yaml controlplane/internal/api/gen controlplane/internal/model/types.go controlplane/web/src/pages/RunDetailPage.tsx controlplane/web/src/api/client.ts
git commit -m "feat(web): run-detail — scenario name title, description, meta cleanup, timeline + chaos window, step messages, SSE token"
```

---

## Task 9: Theme hardening + Runs-list verification (frontend, minor)

**Files:**
- Modify: `web/src/index.css`

- [ ] **Step 1: Root background**

Ensure the dark default background is on the root, not only a wrapper. In `web/src/index.css`, in the base layer add (adjust token to match the theme's background variable):
```css
html, body, #root { min-height: 100%; background-color: hsl(var(--background)); }
```

- [ ] **Step 2: Build + confirm Runs-list column code path**

Run: `cd web && pnpm build`. Confirm `RunsPage.tsx` renders the verdict column via `verdictFromScore(run.score)` (grep). No change needed if it already does (the backend now supplies `score`).

- [ ] **Step 3: Commit**
```bash
cd .. && git add controlplane/web/src/index.css
git commit -m "chore(web): pin dark background to root element"
```

---

## Task 10: Live end-to-end verification (manual, needs the cluster)

**Files:** none.

**Context:** Uses `superpowers:verification-before-completion` — observe real output. Build + load the controlplane image, then verify in-browser.

- [ ] **Step 1: Build + reload the controlplane, rebuild UI**
```bash
cd controlplane && make ui-build && make reload-minikube
kubectl -n dlh-test-fw rollout restart deploy/dlh-controlplane && kubectl -n dlh-test-fw rollout status deploy/dlh-controlplane --timeout=120s
```

- [ ] **Step 2: Verify (port-forward 8080, browser or curl)**
- Runs list `/api/runs`: a finished run now has non-null `score`; the UI VERDICT column shows pass/fail (not "—").
- Run detail: scenario-name title + run-id subtitle + description; meta strip shows Chaos·SLO (+ Priority "—"); verdict values formatted (`3 µs`, `29.6 %`) with a Window column; steps in chronological order with the chaos-window band; `GET /api/runs/{id}/events` returns 200 (no 401).
- A failed run shows the failing threshold highlighted + step message.

- [ ] **Step 3: Commit any verification-driven fixes** (skip if none).

---

## Self-Review notes

- **Spec coverage:** verdict rendering 1a/1b/1c → Tasks 2, 5, 6; steps timeline 2a/2b → Tasks 1, 7, 8; SSE 3 → Task 3; scenario description 4 → Tasks 4, 8(step1); header/meta 5 → Task 8; states 6 → Task 8 (messages/running) + 6 (fail highlight); theme 7 → Task 9.
- **Priority cell** is display-only and renders "—" until the priority spec lands (Task 8 step 3 adds the optional `priority` field).
- **Deferred/uncertain:** the report.json **recovery**-window field name is confirmed at implementation time against a real report (chaos window fields `chaos_window_start/end` are verified present); if recovery fields differ, omit the recovery band (chaos band is the must-have). The `deriveTargetType` rule is duplicated Go/TS — reuse the existing one.
- **Type consistency:** `formatMetricByName`, `timelineLayout`, `windowBand`, `ScenarioDescription`, `NewVerdictCache`/`Score`, `bearerOrQueryToken` are each defined once and referenced consistently.
