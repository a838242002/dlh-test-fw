# Plan 3 — Verdict Go Binary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go CLI binary `verdict` that consumes an SLO YAML + workflow timing parameters, queries VictoriaMetrics over windows (baseline / chaos / recovery / full), polls the ChaosResult CR with bounded retry, renders `report.json` + a self-contained `report.html`, patches a `dlh-result-<workflow>` ConfigMap, and exits 0 (Pass) or 1 (Fail).

**Architecture:** Standard `cmd/<bin>` + `internal/*` Go layout. Pure-functional core packages (slo parser, window computation, threshold evaluation, report rendering) with thin shells around external IO (Prometheus client, kubernetes client). Each internal package is unit-tested without network. The CLI binary wires them together. A single Dockerfile produces a distroless image suitable for use inside the `verdict/slo-eval` WorkflowTemplate (Plan 4).

**Tech Stack:** Go 1.22, `github.com/prometheus/client_golang/api`, `k8s.io/client-go`, `gopkg.in/yaml.v3`, `html/template`, Go's stdlib `testing`. Build with multi-stage Docker → distroless final image.

**Prerequisites:** Plans 1 and 2 complete. Verdict's image tag (`platform.verdict.tag`) in `helm/dlh-test-fw/values.yaml` is `0.1.0` — we'll build and tag accordingly so the WorkflowTemplate in Plan 4 just picks it up.

**Out of scope:** WorkflowTemplate that invokes this binary (Plan 4). HTML report styling beyond the minimal banner-table-buttons layout spec calls for (we can polish later). Push to a real registry (we only build locally and `minikube image load` it).

---

## File Structure

```
verdict-job/
├── go.mod
├── go.sum
├── Dockerfile
├── Makefile                            # build, test, image, load-into-minikube
├── cmd/
│   └── verdict/
│       └── main.go                     # flag parsing, wiring, exit code
├── internal/
│   ├── slo/
│   │   ├── slo.go                      # types + YAML parse + validation
│   │   └── slo_test.go
│   ├── window/
│   │   ├── window.go                   # compute baseline/chaos/recovery/full from inputs
│   │   └── window_test.go
│   ├── prom/
│   │   ├── prom.go                     # PromAPI interface + http impl
│   │   ├── prom_test.go                # against httptest.Server stub
│   │   └── fake.go                     # in-memory fake for higher-layer tests
│   ├── chaosresult/
│   │   ├── chaosresult.go              # K8s client wrapper with bounded retry
│   │   └── chaosresult_test.go         # against fake.NewSimpleDynamicClient
│   ├── eval/
│   │   ├── eval.go                     # evaluate thresholds + raw_promql + chaos verdict → Overall
│   │   └── eval_test.go
│   ├── report/
│   │   ├── report.go                   # JSON struct + HTML template + Render
│   │   ├── report_test.go              # golden-file test for HTML
│   │   ├── template.html.tmpl
│   │   └── testdata/
│   │       └── golden-report.html      # produced by test; regenerate with -update
│   └── publish/
│       ├── publish.go                  # patch ConfigMap dlh-result-<workflow>
│       └── publish_test.go             # fake clientset
└── testdata/
    ├── slo-simple.yaml                 # used by slo + eval tests
    └── slo-invalid.yaml
```

Responsibilities:
- `internal/slo`: parse SLO YAML into typed structs; validate (e.g. `lt`/`gt` exactly one set; `window` enum; `raw_window` required iff `raw_promql` set).
- `internal/window`: pure arithmetic on `load_start_ts`, `chaos_start_after`, `chaos_duration`, `load_duration` → `Window{Start, End}` for each named window.
- `internal/prom`: tiny interface `Query(ctx, q string, t time.Time) (float64, error)`; HTTP impl + test fake.
- `internal/chaosresult`: read `ChaosResult.status.experimentStatus.verdict`; bounded retry while it equals `"Awaited"` (max 30s, configurable).
- `internal/eval`: combine threshold results + raw_promql + chaos verdict → overall pass/fail; returns a rich struct for the report.
- `internal/report`: render JSON + HTML from the eval result.
- `internal/publish`: patch (or create) the result ConfigMap with the report summary.
- `cmd/verdict/main.go`: flag parsing, wiring, exit code.

---

## Task 1: Module bootstrap

**Files:**
- Create: `verdict-job/go.mod`
- Create: `verdict-job/Makefile`
- Create: `verdict-job/cmd/verdict/main.go` (stub)

- [ ] **Step 1: Initialize module**

```bash
cd /Users/allen/repo/dlh-test-fw
mkdir -p verdict-job && cd verdict-job
go mod init github.com/dlh/dlh-test-fw/verdict-job
```

- [ ] **Step 2: Add a stub `cmd/verdict/main.go`**

```go
package main

import "fmt"

func main() {
    fmt.Println("verdict: not yet implemented")
}
```

- [ ] **Step 3: Write `verdict-job/Makefile`**

```makefile
SHELL := /usr/bin/env bash
.PHONY: test build image load-image clean

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/verdict ./cmd/verdict

image:
	docker build -t dlh-verdict:0.1.0 .

# Load into the running minikube so chart's image ref resolves.
load-image: image
	minikube image load dlh-verdict:0.1.0

clean:
	rm -rf bin/
```

- [ ] **Step 4: Sanity build**

```bash
cd /Users/allen/repo/dlh-test-fw/verdict-job
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add verdict-job/go.mod verdict-job/cmd/verdict/main.go verdict-job/Makefile
git commit -m "verdict: bootstrap go module and stub main"
```

---

## Task 2: SLO YAML model (TDD)

**Files:**
- Create: `verdict-job/internal/slo/slo.go`
- Create: `verdict-job/internal/slo/slo_test.go`
- Create: `verdict-job/testdata/slo-simple.yaml`
- Create: `verdict-job/testdata/slo-invalid.yaml`

- [ ] **Step 1: Write `testdata/slo-simple.yaml`**

```yaml
thresholds:
- metric: p95-latency-chaos
  query: histogram_quantile(0.95, sum(rate(k6_http_req_duration_seconds_bucket{scenario="x"}[1m])) by (le))
  lt: 0.5
  window: chaos
- metric: error-rate-recovery
  query: sum(rate(k6_http_reqs_total{scenario="x",status!~"2.."}[1m])) / sum(rate(k6_http_reqs_total{scenario="x"}[1m]))
  lt: 0.01
  window: recovery
raw_promql: sum(up{job="my-svc"}) > 0
raw_window: chaos
```

- [ ] **Step 2: Write `testdata/slo-invalid.yaml`**

```yaml
thresholds:
- metric: bad
  query: foo
  # neither lt nor gt — invalid
  window: chaos
```

- [ ] **Step 3: Write the failing test `slo_test.go`**

```go
package slo

import (
    "os"
    "testing"
)

func TestParseSimple(t *testing.T) {
    b, err := os.ReadFile("../../testdata/slo-simple.yaml")
    if err != nil {
        t.Fatal(err)
    }
    s, err := Parse(b)
    if err != nil {
        t.Fatalf("Parse returned error: %v", err)
    }
    if got, want := len(s.Thresholds), 2; got != want {
        t.Fatalf("Thresholds len: got %d want %d", got, want)
    }
    if s.Thresholds[0].Window != WindowChaos {
        t.Errorf("Thresholds[0].Window: got %q want %q", s.Thresholds[0].Window, WindowChaos)
    }
    if s.Thresholds[0].LT == nil || *s.Thresholds[0].LT != 0.5 {
        t.Errorf("Thresholds[0].LT: got %v want 0.5", s.Thresholds[0].LT)
    }
    if s.RawPromQL == "" || s.RawWindow != WindowChaos {
        t.Errorf("raw_promql/raw_window not parsed: %+v / %q", s.RawPromQL, s.RawWindow)
    }
}

func TestParseInvalidMissingBound(t *testing.T) {
    b, err := os.ReadFile("../../testdata/slo-invalid.yaml")
    if err != nil {
        t.Fatal(err)
    }
    if _, err := Parse(b); err == nil {
        t.Fatalf("expected validation error, got nil")
    }
}

func TestParseRawPromQLRequiresWindow(t *testing.T) {
    yaml := []byte(`raw_promql: "up > 0"` + "\n")
    if _, err := Parse(yaml); err == nil {
        t.Fatalf("raw_promql without raw_window should fail validation")
    }
}
```

- [ ] **Step 4: Run test — expect compile failure (no implementation yet)**

```bash
cd /Users/allen/repo/dlh-test-fw/verdict-job
go test ./internal/slo/... -v
```

Expected: build fails because package has no source.

- [ ] **Step 5: Write `internal/slo/slo.go`**

```go
// Package slo parses and validates SLO definitions from scenario YAML.
package slo

import (
    "errors"
    "fmt"

    "gopkg.in/yaml.v3"
)

type Window string

const (
    WindowBaseline Window = "baseline"
    WindowChaos    Window = "chaos"
    WindowRecovery Window = "recovery"
    WindowFull     Window = "full"
)

func (w Window) Valid() bool {
    switch w {
    case WindowBaseline, WindowChaos, WindowRecovery, WindowFull:
        return true
    }
    return false
}

type Threshold struct {
    Metric string   `yaml:"metric"`
    Query  string   `yaml:"query"`
    LT     *float64 `yaml:"lt,omitempty"`
    GT     *float64 `yaml:"gt,omitempty"`
    Window Window   `yaml:"window"`
}

type SLO struct {
    Thresholds []Threshold `yaml:"thresholds"`
    RawPromQL  string      `yaml:"raw_promql,omitempty"`
    RawWindow  Window      `yaml:"raw_window,omitempty"`
}

func Parse(b []byte) (*SLO, error) {
    var s SLO
    if err := yaml.Unmarshal(b, &s); err != nil {
        return nil, fmt.Errorf("yaml: %w", err)
    }
    if err := s.Validate(); err != nil {
        return nil, err
    }
    return &s, nil
}

func (s *SLO) Validate() error {
    if len(s.Thresholds) == 0 && s.RawPromQL == "" {
        return errors.New("slo: at least one threshold or raw_promql required")
    }
    for i, t := range s.Thresholds {
        if t.Metric == "" {
            return fmt.Errorf("slo: threshold[%d].metric empty", i)
        }
        if t.Query == "" {
            return fmt.Errorf("slo: threshold[%d].query empty", i)
        }
        if (t.LT == nil) == (t.GT == nil) {
            return fmt.Errorf("slo: threshold[%d] (%s): exactly one of lt/gt required", i, t.Metric)
        }
        if !t.Window.Valid() {
            return fmt.Errorf("slo: threshold[%d] (%s): invalid window %q", i, t.Metric, t.Window)
        }
    }
    if s.RawPromQL != "" && !s.RawWindow.Valid() {
        return errors.New("slo: raw_window required (and valid) when raw_promql set")
    }
    return nil
}
```

- [ ] **Step 6: `go mod tidy`**

```bash
cd /Users/allen/repo/dlh-test-fw/verdict-job
go mod tidy
```

- [ ] **Step 7: Run tests — expect pass**

```bash
go test ./internal/slo/... -v
```

Expected: all three tests pass.

- [ ] **Step 8: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add verdict-job/internal/slo verdict-job/testdata verdict-job/go.mod verdict-job/go.sum
git commit -m "verdict(slo): parse + validate SLO YAML"
```

---

## Task 3: Window computation (TDD)

**Files:**
- Create: `verdict-job/internal/window/window.go`
- Create: `verdict-job/internal/window/window_test.go`

- [ ] **Step 1: Write the failing test**

```go
package window

import (
    "testing"
    "time"

    "github.com/dlh/dlh-test-fw/verdict-job/internal/slo"
)

func TestCompute(t *testing.T) {
    loadStart := time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC)
    p := Params{
        LoadStart:       loadStart,
        ChaosStartAfter: 30 * time.Second,
        ChaosDuration:   60 * time.Second,
        LoadDuration:    180 * time.Second,
    }

    cases := []struct {
        name string
        w    slo.Window
        start, end time.Time
    }{
        {"baseline", slo.WindowBaseline, loadStart, loadStart.Add(30 * time.Second)},
        {"chaos",    slo.WindowChaos,    loadStart.Add(30 * time.Second), loadStart.Add(90 * time.Second)},
        {"recovery", slo.WindowRecovery, loadStart.Add(90 * time.Second), loadStart.Add(180 * time.Second)},
        {"full",     slo.WindowFull,     loadStart, loadStart.Add(180 * time.Second)},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            w, err := Compute(p, c.w)
            if err != nil {
                t.Fatal(err)
            }
            if !w.Start.Equal(c.start) || !w.End.Equal(c.end) {
                t.Errorf("%s: got [%v,%v] want [%v,%v]", c.name, w.Start, w.End, c.start, c.end)
            }
        })
    }
}

func TestValidateRejectsChaosOverflow(t *testing.T) {
    p := Params{
        ChaosStartAfter: 60 * time.Second,
        ChaosDuration:   120 * time.Second,
        LoadDuration:    150 * time.Second,
    }
    if err := p.Validate(); err == nil {
        t.Fatal("expected validation error: chaos_start_after + chaos_duration > load_duration")
    }
}
```

- [ ] **Step 2: Run — expect compile failure**

```bash
go test ./internal/window/... -v
```

- [ ] **Step 3: Implement `internal/window/window.go`**

```go
// Package window computes the baseline/chaos/recovery/full windows of a scenario run.
package window

import (
    "fmt"
    "time"

    "github.com/dlh/dlh-test-fw/verdict-job/internal/slo"
)

type Params struct {
    LoadStart       time.Time
    ChaosStartAfter time.Duration
    ChaosDuration   time.Duration
    LoadDuration    time.Duration
}

type Window struct {
    Start time.Time
    End   time.Time
}

func (p Params) Validate() error {
    if p.ChaosStartAfter < 0 || p.ChaosDuration <= 0 || p.LoadDuration <= 0 {
        return fmt.Errorf("window: all durations must be positive (chaos_start_after may be 0)")
    }
    if p.ChaosStartAfter+p.ChaosDuration > p.LoadDuration {
        return fmt.Errorf("window: chaos_start_after (%v) + chaos_duration (%v) > load_duration (%v)",
            p.ChaosStartAfter, p.ChaosDuration, p.LoadDuration)
    }
    return nil
}

func Compute(p Params, w slo.Window) (Window, error) {
    if err := p.Validate(); err != nil {
        return Window{}, err
    }
    chaosStart := p.LoadStart.Add(p.ChaosStartAfter)
    chaosEnd := chaosStart.Add(p.ChaosDuration)
    loadEnd := p.LoadStart.Add(p.LoadDuration)
    switch w {
    case slo.WindowBaseline:
        return Window{Start: p.LoadStart, End: chaosStart}, nil
    case slo.WindowChaos:
        return Window{Start: chaosStart, End: chaosEnd}, nil
    case slo.WindowRecovery:
        return Window{Start: chaosEnd, End: loadEnd}, nil
    case slo.WindowFull:
        return Window{Start: p.LoadStart, End: loadEnd}, nil
    default:
        return Window{}, fmt.Errorf("window: unknown window %q", w)
    }
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/window/... -v
```

- [ ] **Step 5: Commit**

```bash
git add verdict-job/internal/window
git commit -m "verdict(window): compute baseline/chaos/recovery/full windows"
```

---

## Task 4: Prometheus client (TDD with httptest)

**Files:**
- Create: `verdict-job/internal/prom/prom.go`
- Create: `verdict-job/internal/prom/prom_test.go`
- Create: `verdict-job/internal/prom/fake.go`

- [ ] **Step 1: Write the failing test (against httptest.Server)**

```go
package prom

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"
)

func TestQueryAtParsesScalarLikeResult(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/api/v1/query" {
            t.Errorf("unexpected path %s", r.URL.Path)
        }
        if q := r.URL.Query().Get("query"); q != "up" {
            t.Errorf("unexpected query %q", q)
        }
        json.NewEncoder(w).Encode(map[string]any{
            "status": "success",
            "data": map[string]any{
                "resultType": "vector",
                "result": []map[string]any{{
                    "metric": map[string]string{},
                    "value":  []any{1700000000.0, "42.5"},
                }},
            },
        })
    }))
    defer srv.Close()

    c := New(srv.URL)
    v, err := c.QueryAt(context.Background(), "up", time.Unix(1700000000, 0))
    if err != nil {
        t.Fatal(err)
    }
    if v != 42.5 {
        t.Errorf("got %v want 42.5", v)
    }
}

func TestQueryAtEmptyResultReturnsZero(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(map[string]any{
            "status": "success",
            "data":   map[string]any{"resultType": "vector", "result": []any{}},
        })
    }))
    defer srv.Close()
    v, err := New(srv.URL).QueryAt(context.Background(), "up", time.Now())
    if err != nil {
        t.Fatal(err)
    }
    if v != 0 {
        t.Errorf("empty result: got %v want 0", v)
    }
}
```

- [ ] **Step 2: Run — expect compile failure**

- [ ] **Step 3: Implement `internal/prom/prom.go`**

```go
// Package prom is a tiny PromQL client. Vector-instant queries only — that's all verdict needs.
package prom

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strconv"
    "time"
)

type API interface {
    QueryAt(ctx context.Context, q string, t time.Time) (float64, error)
}

type Client struct {
    BaseURL string
    HTTP    *http.Client
}

func New(baseURL string) *Client {
    return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type queryResp struct {
    Status string `json:"status"`
    Data   struct {
        Result []struct {
            Value [2]any `json:"value"`
        } `json:"result"`
    } `json:"data"`
}

func (c *Client) QueryAt(ctx context.Context, q string, t time.Time) (float64, error) {
    u := c.BaseURL + "/api/v1/query?query=" + url.QueryEscape(q) +
        "&time=" + strconv.FormatInt(t.Unix(), 10)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    if err != nil {
        return 0, err
    }
    resp, err := c.HTTP.Do(req)
    if err != nil {
        return 0, err
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    if resp.StatusCode != 200 {
        return 0, fmt.Errorf("prom: HTTP %d: %s", resp.StatusCode, body)
    }
    var qr queryResp
    if err := json.Unmarshal(body, &qr); err != nil {
        return 0, fmt.Errorf("prom: decode: %w", err)
    }
    if qr.Status != "success" {
        return 0, fmt.Errorf("prom: status=%s body=%s", qr.Status, body)
    }
    if len(qr.Data.Result) == 0 {
        return 0, nil // empty result → 0 (caller decides if that's pass or fail)
    }
    sv, ok := qr.Data.Result[0].Value[1].(string)
    if !ok {
        return 0, fmt.Errorf("prom: value field not a string: %v", qr.Data.Result[0].Value[1])
    }
    return strconv.ParseFloat(sv, 64)
}
```

- [ ] **Step 4: Implement `internal/prom/fake.go`**

```go
package prom

import (
    "context"
    "time"
)

type Fake struct {
    Values map[string]float64 // query string → value
}

func (f *Fake) QueryAt(_ context.Context, q string, _ time.Time) (float64, error) {
    if v, ok := f.Values[q]; ok {
        return v, nil
    }
    return 0, nil
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/prom/... -v
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add verdict-job/internal/prom
git commit -m "verdict(prom): vector-instant query client with httptest coverage"
```

---

## Task 5: ChaosResult client (TDD with fake dynamic client)

**Files:**
- Create: `verdict-job/internal/chaosresult/chaosresult.go`
- Create: `verdict-job/internal/chaosresult/chaosresult_test.go`

- [ ] **Step 1: Add `k8s.io/client-go` and `apimachinery` deps via go.mod**

```bash
cd /Users/allen/repo/dlh-test-fw/verdict-job
go get k8s.io/client-go@v0.30.0 k8s.io/apimachinery@v0.30.0
```

- [ ] **Step 2: Write the failing test**

```go
package chaosresult

import (
    "context"
    "testing"
    "time"

    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/schema"
    dynamicfake "k8s.io/client-go/dynamic/fake"
)

var gvr = schema.GroupVersionResource{Group: "litmuschaos.io", Version: "v1alpha1", Resource: "chaosresults"}

func mkCR(verdict string) *unstructured.Unstructured {
    return &unstructured.Unstructured{Object: map[string]any{
        "apiVersion": "litmuschaos.io/v1alpha1",
        "kind":       "ChaosResult",
        "metadata":   map[string]any{"name": "cr1", "namespace": "dlh-test-fw"},
        "status": map[string]any{
            "experimentStatus": map[string]any{"verdict": verdict},
        },
    }}
}

func TestGetVerdictPass(t *testing.T) {
    scheme := runtime.NewScheme()
    dc := dynamicfake.NewSimpleDynamicClient(scheme, mkCR("Pass"))
    c := &Client{Dyn: dc, GVR: gvr, Namespace: "dlh-test-fw"}
    v, err := c.GetVerdict(context.Background(), "cr1", 5*time.Second)
    if err != nil { t.Fatal(err) }
    if v != "Pass" { t.Errorf("got %q want Pass", v) }
}

func TestGetVerdictAwaitedTimesOut(t *testing.T) {
    scheme := runtime.NewScheme()
    dc := dynamicfake.NewSimpleDynamicClient(scheme, mkCR("Awaited"))
    c := &Client{Dyn: dc, GVR: gvr, Namespace: "dlh-test-fw", PollInterval: 50 * time.Millisecond}
    _, err := c.GetVerdict(context.Background(), "cr1", 200*time.Millisecond)
    if err == nil { t.Fatal("expected timeout error") }
}
```

- [ ] **Step 3: Run — expect compile failure**

- [ ] **Step 4: Implement `internal/chaosresult/chaosresult.go`**

```go
// Package chaosresult reads ChaosResult.status.experimentStatus.verdict from the Litmus CRD.
package chaosresult

import (
    "context"
    "fmt"
    "time"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/client-go/dynamic"
)

type Client struct {
    Dyn          dynamic.Interface
    GVR          schema.GroupVersionResource
    Namespace    string
    PollInterval time.Duration // default 2s
}

// GetVerdict reads the named ChaosResult. While status reads "Awaited", retries until timeout.
// Returns "Pass", "Fail", "Stopped", etc. (whatever Litmus wrote).
func (c *Client) GetVerdict(ctx context.Context, name string, timeout time.Duration) (string, error) {
    interval := c.PollInterval
    if interval == 0 {
        interval = 2 * time.Second
    }
    deadline := time.Now().Add(timeout)
    for {
        u, err := c.Dyn.Resource(c.GVR).Namespace(c.Namespace).Get(ctx, name, metav1.GetOptions{})
        if err != nil {
            return "", fmt.Errorf("chaosresult: get %s: %w", name, err)
        }
        v, _ := nested(u, "status", "experimentStatus", "verdict")
        s, _ := v.(string)
        if s != "" && s != "Awaited" {
            return s, nil
        }
        if time.Now().After(deadline) {
            return "", fmt.Errorf("chaosresult: %s still %q after %v", name, s, timeout)
        }
        select {
        case <-ctx.Done():
            return "", ctx.Err()
        case <-time.After(interval):
        }
    }
}

func nested(u *unstructured.Unstructured, path ...string) (any, bool) {
    var cur any = u.Object
    for _, k := range path {
        m, ok := cur.(map[string]any)
        if !ok {
            return nil, false
        }
        cur, ok = m[k]
        if !ok {
            return nil, false
        }
    }
    return cur, true
}
```

- [ ] **Step 5: Run tests**

```bash
go mod tidy
go test ./internal/chaosresult/... -v
```

Expected: pass. The "Awaited" test should take ~200ms.

- [ ] **Step 6: Commit**

```bash
git add verdict-job/internal/chaosresult verdict-job/go.mod verdict-job/go.sum
git commit -m "verdict(chaosresult): read ChaosResult verdict with bounded retry on Awaited"
```

---

## Task 6: Evaluation engine (TDD)

**Files:**
- Create: `verdict-job/internal/eval/eval.go`
- Create: `verdict-job/internal/eval/eval_test.go`

- [ ] **Step 1: Write the failing test**

```go
package eval

import (
    "context"
    "testing"
    "time"

    "github.com/dlh/dlh-test-fw/verdict-job/internal/prom"
    "github.com/dlh/dlh-test-fw/verdict-job/internal/slo"
    "github.com/dlh/dlh-test-fw/verdict-job/internal/window"
)

func ptr(f float64) *float64 { return &f }

func TestEvaluatePassAllGreen(t *testing.T) {
    s := &slo.SLO{
        Thresholds: []slo.Threshold{
            {Metric: "lat", Query: "Q1", LT: ptr(0.5), Window: slo.WindowChaos},
            {Metric: "err", Query: "Q2", LT: ptr(0.01), Window: slo.WindowRecovery},
        },
        RawPromQL: "Q3",
        RawWindow: slo.WindowChaos,
    }
    fake := &prom.Fake{Values: map[string]float64{"Q1": 0.2, "Q2": 0.001, "Q3": 1}}
    p := window.Params{
        LoadStart: time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC),
        ChaosStartAfter: 10 * time.Second,
        ChaosDuration: 30 * time.Second,
        LoadDuration: 120 * time.Second,
    }
    r, err := Evaluate(context.Background(), s, fake, p, "Pass")
    if err != nil { t.Fatal(err) }
    if !r.Overall { t.Fatalf("expected Pass, got %+v", r) }
    for _, tr := range r.Thresholds {
        if !tr.Passed { t.Errorf("threshold %s should pass: %+v", tr.Metric, tr) }
    }
    if !r.RawPromQLPass { t.Error("rawPromQL should pass") }
    if r.ChaosVerdict != "Pass" { t.Error("chaos verdict") }
}

func TestEvaluateFailWhenThresholdExceeded(t *testing.T) {
    s := &slo.SLO{Thresholds: []slo.Threshold{
        {Metric: "lat", Query: "Q1", LT: ptr(0.5), Window: slo.WindowChaos},
    }}
    fake := &prom.Fake{Values: map[string]float64{"Q1": 0.9}}
    p := window.Params{
        LoadStart: time.Now(),
        ChaosStartAfter: 10 * time.Second,
        ChaosDuration: 30 * time.Second,
        LoadDuration: 120 * time.Second,
    }
    r, err := Evaluate(context.Background(), s, fake, p, "Pass")
    if err != nil { t.Fatal(err) }
    if r.Overall { t.Fatalf("expected Fail, got Pass: %+v", r) }
}

func TestEvaluateFailWhenChaosVerdictNotPass(t *testing.T) {
    s := &slo.SLO{Thresholds: []slo.Threshold{
        {Metric: "lat", Query: "Q1", LT: ptr(0.5), Window: slo.WindowChaos},
    }}
    fake := &prom.Fake{Values: map[string]float64{"Q1": 0.1}}
    p := window.Params{
        LoadStart: time.Now(),
        ChaosStartAfter: 10 * time.Second,
        ChaosDuration: 30 * time.Second,
        LoadDuration: 120 * time.Second,
    }
    r, err := Evaluate(context.Background(), s, fake, p, "Fail")
    if err != nil { t.Fatal(err) }
    if r.Overall { t.Fatal("chaos Fail must force Overall Fail") }
}

func TestEvaluateGTBound(t *testing.T) {
    s := &slo.SLO{Thresholds: []slo.Threshold{
        {Metric: "throughput", Query: "Q1", GT: ptr(100), Window: slo.WindowChaos},
    }}
    fake := &prom.Fake{Values: map[string]float64{"Q1": 50}}
    p := window.Params{
        LoadStart: time.Now(), ChaosStartAfter: 10*time.Second, ChaosDuration: 30*time.Second, LoadDuration: 120*time.Second,
    }
    r, _ := Evaluate(context.Background(), s, fake, p, "Pass")
    if r.Overall { t.Fatal("50 > 100 is false, should fail") }
}
```

- [ ] **Step 2: Implement `internal/eval/eval.go`**

```go
// Package eval combines threshold checks, raw PromQL, and chaos verdict into an overall result.
package eval

import (
    "context"

    "github.com/dlh/dlh-test-fw/verdict-job/internal/prom"
    "github.com/dlh/dlh-test-fw/verdict-job/internal/slo"
    "github.com/dlh/dlh-test-fw/verdict-job/internal/window"
)

type ThresholdResult struct {
    Metric      string  `json:"metric"`
    Query       string  `json:"query"`
    Window      string  `json:"window"`
    WindowStart string  `json:"window_start"`
    WindowEnd   string  `json:"window_end"`
    Value       float64 `json:"value"`
    LT          *float64 `json:"lt,omitempty"`
    GT          *float64 `json:"gt,omitempty"`
    Passed      bool    `json:"passed"`
}

type Result struct {
    Overall        bool              `json:"overall"`
    Thresholds     []ThresholdResult `json:"thresholds"`
    RawPromQL      string            `json:"raw_promql,omitempty"`
    RawPromQLValue float64           `json:"raw_promql_value,omitempty"`
    RawPromQLPass  bool              `json:"raw_promql_pass,omitempty"`
    ChaosVerdict   string            `json:"chaos_verdict"`
}

func Evaluate(ctx context.Context, s *slo.SLO, p prom.API, win window.Params, chaosVerdict string) (*Result, error) {
    r := &Result{ChaosVerdict: chaosVerdict, Overall: true}

    for _, t := range s.Thresholds {
        w, err := window.Compute(win, t.Window)
        if err != nil {
            return nil, err
        }
        v, err := p.QueryAt(ctx, t.Query, w.End)
        if err != nil {
            return nil, err
        }
        passed := false
        switch {
        case t.LT != nil:
            passed = v < *t.LT
        case t.GT != nil:
            passed = v > *t.GT
        }
        r.Thresholds = append(r.Thresholds, ThresholdResult{
            Metric: t.Metric, Query: t.Query, Window: string(t.Window),
            WindowStart: w.Start.UTC().Format("2006-01-02T15:04:05Z"),
            WindowEnd:   w.End.UTC().Format("2006-01-02T15:04:05Z"),
            Value: v, LT: t.LT, GT: t.GT, Passed: passed,
        })
        if !passed {
            r.Overall = false
        }
    }

    if s.RawPromQL != "" {
        w, err := window.Compute(win, s.RawWindow)
        if err != nil {
            return nil, err
        }
        v, err := p.QueryAt(ctx, s.RawPromQL, w.End)
        if err != nil {
            return nil, err
        }
        r.RawPromQL = s.RawPromQL
        r.RawPromQLValue = v
        r.RawPromQLPass = v == 1
        if !r.RawPromQLPass {
            r.Overall = false
        }
    }

    if chaosVerdict != "Pass" {
        r.Overall = false
    }
    return r, nil
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/eval/... -v
```

Expected: all four pass.

- [ ] **Step 4: Commit**

```bash
git add verdict-job/internal/eval
git commit -m "verdict(eval): combine thresholds + raw_promql + chaos verdict"
```

---

## Task 7: Report rendering (TDD with golden file)

**Files:**
- Create: `verdict-job/internal/report/report.go`
- Create: `verdict-job/internal/report/template.html.tmpl`
- Create: `verdict-job/internal/report/report_test.go`
- Create: `verdict-job/internal/report/testdata/golden-report.html` (generated by test)

- [ ] **Step 1: Write `template.html.tmpl`**

```html
<!doctype html>
<html><head><meta charset="utf-8"><title>DLH Verdict — {{.ScenarioName}}</title>
<style>
body{font-family:system-ui,sans-serif;max-width:960px;margin:2rem auto;padding:0 1rem}
.banner{padding:1.5rem;border-radius:.5rem;color:#fff;font-size:1.5rem;font-weight:600}
.pass{background:#16a34a}.fail{background:#dc2626}
table{width:100%;border-collapse:collapse;margin-top:1rem}
th,td{border:1px solid #ddd;padding:.5rem;text-align:left;font-size:.9rem}
th{background:#f3f4f6}
.btnrow{margin-top:1rem;display:flex;gap:.5rem}
.btn{padding:.5rem 1rem;border:1px solid #888;border-radius:.25rem;text-decoration:none;color:#222}
</style></head><body>
<div class="banner {{if .Overall}}pass{{else}}fail{{end}}">
  {{if .Overall}}PASS{{else}}FAIL{{end}} — {{.ScenarioName}}
  <div style="font-size:.9rem;font-weight:400;margin-top:.25rem">
    chaos verdict: {{.ChaosVerdict}} · duration: {{.LoadDurationSec}}s
  </div>
</div>
<h2>Thresholds</h2>
<table>
<tr><th>Metric</th><th>Window</th><th>Value</th><th>Bound</th><th>Status</th></tr>
{{range .Thresholds}}
<tr>
  <td>{{.Metric}}<br><small><code>{{.Query}}</code></small></td>
  <td>{{.Window}}<br><small>{{.WindowStart}} → {{.WindowEnd}}</small></td>
  <td>{{printf "%.4f" .Value}}</td>
  <td>{{if .LT}}&lt; {{printf "%g" (deref .LT)}}{{end}}{{if .GT}}&gt; {{printf "%g" (deref .GT)}}{{end}}</td>
  <td>{{if .Passed}}PASS{{else}}FAIL{{end}}</td>
</tr>{{end}}
</table>
{{if .RawPromQL}}
<h2>Raw PromQL</h2>
<table>
<tr><th>Query</th><th>Value</th><th>Status</th></tr>
<tr><td><code>{{.RawPromQL}}</code></td><td>{{printf "%g" .RawPromQLValue}}</td>
<td>{{if .RawPromQLPass}}PASS{{else}}FAIL{{end}}</td></tr>
</table>
{{end}}
<div class="btnrow">
  {{if .GrafanaURL}}<a class="btn" href="{{.GrafanaURL}}">Open in Grafana</a>{{end}}
  {{if .ArgoURL}}<a class="btn" href="{{.ArgoURL}}">Argo Workflow</a>{{end}}
  {{if .JSONURL}}<a class="btn" href="{{.JSONURL}}">Download JSON</a>{{end}}
</div>
</body></html>
```

- [ ] **Step 2: Write `report.go`**

```go
// Package report renders JSON + self-contained HTML reports from an eval.Result.
package report

import (
    _ "embed"
    "encoding/json"
    "fmt"
    "html/template"
    "io"
    "os"
    "path/filepath"

    "github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
)

//go:embed template.html.tmpl
var htmlTmpl string

type View struct {
    *eval.Result
    ScenarioName    string
    LoadDurationSec int
    GrafanaURL      string
    ArgoURL         string
    JSONURL         string
}

var funcs = template.FuncMap{
    "deref": func(p *float64) float64 {
        if p == nil { return 0 }
        return *p
    },
}

func RenderJSON(w io.Writer, v View) error {
    enc := json.NewEncoder(w)
    enc.SetIndent("", "  ")
    return enc.Encode(v)
}

func RenderHTML(w io.Writer, v View) error {
    t, err := template.New("r").Funcs(funcs).Parse(htmlTmpl)
    if err != nil { return err }
    return t.Execute(w, v)
}

// Write writes both report.json and report.html under dir.
func Write(dir string, v View) (string, string, error) {
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return "", "", err
    }
    jpath := filepath.Join(dir, "report.json")
    hpath := filepath.Join(dir, "report.html")
    jf, err := os.Create(jpath)
    if err != nil { return "", "", err }
    defer jf.Close()
    if err := RenderJSON(jf, v); err != nil { return "", "", err }
    hf, err := os.Create(hpath)
    if err != nil { return "", "", err }
    defer hf.Close()
    if err := RenderHTML(hf, v); err != nil { return "", "", err }
    return jpath, hpath, nil
}

func MustOpen(p string) *os.File {
    f, err := os.Open(p)
    if err != nil { panic(fmt.Errorf("open %s: %w", p, err)) }
    return f
}
```

- [ ] **Step 3: Write `report_test.go` with golden-file convention**

```go
package report

import (
    "bytes"
    "flag"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
)

var update = flag.Bool("update", false, "rewrite golden files")

func sampleView() View {
    lt := 0.5
    return View{
        Result: &eval.Result{
            Overall: true,
            ChaosVerdict: "Pass",
            Thresholds: []eval.ThresholdResult{{
                Metric: "p95-latency", Query: "Q1", Window: "chaos",
                WindowStart: "2026-05-16T10:00:30Z", WindowEnd: "2026-05-16T10:01:00Z",
                Value: 0.42, LT: &lt, Passed: true,
            }},
        },
        ScenarioName: "demo", LoadDurationSec: 120,
        GrafanaURL: "http://grafana/d/x", ArgoURL: "http://argo/wf",
    }
}

func TestRenderHTMLGolden(t *testing.T) {
    var buf bytes.Buffer
    if err := RenderHTML(&buf, sampleView()); err != nil { t.Fatal(err) }
    got := buf.Bytes()
    p := filepath.Join("testdata", "golden-report.html")
    if *update {
        _ = os.MkdirAll("testdata", 0o755)
        if err := os.WriteFile(p, got, 0o644); err != nil { t.Fatal(err) }
        return
    }
    want, err := os.ReadFile(p)
    if err != nil { t.Fatalf("read golden (run with -update to create): %v", err) }
    if !bytes.Equal(got, want) {
        t.Fatalf("HTML mismatch (run -update to refresh).\nGot:\n%s", string(got))
    }
}

func TestRenderJSONShape(t *testing.T) {
    var buf bytes.Buffer
    if err := RenderJSON(&buf, sampleView()); err != nil { t.Fatal(err) }
    s := buf.String()
    for _, want := range []string{`"overall": true`, `"chaos_verdict": "Pass"`, `"metric": "p95-latency"`} {
        if !strings.Contains(s, want) {
            t.Errorf("JSON missing %q.\n%s", want, s)
        }
    }
}
```

- [ ] **Step 4: Generate the golden file**

```bash
cd /Users/allen/repo/dlh-test-fw/verdict-job
go test ./internal/report/... -run TestRenderHTMLGolden -update
```

Inspect `internal/report/testdata/golden-report.html` — make sure it renders cleanly when opened in a browser.

- [ ] **Step 5: Run all report tests without `-update` — expect pass**

```bash
go test ./internal/report/... -v
```

- [ ] **Step 6: Commit**

```bash
git add verdict-job/internal/report
git commit -m "verdict(report): JSON + self-contained HTML report with golden test"
```

---

## Task 8: ConfigMap publisher (TDD with fake clientset)

**Files:**
- Create: `verdict-job/internal/publish/publish.go`
- Create: `verdict-job/internal/publish/publish_test.go`

- [ ] **Step 1: Write the failing test**

```go
package publish

import (
    "context"
    "encoding/json"
    "testing"

    "github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes/fake"
)

func TestPublishCreatesConfigMap(t *testing.T) {
    cs := fake.NewSimpleClientset()
    p := &Publisher{Cs: cs, Namespace: "dlh-test-fw"}
    r := &eval.Result{Overall: true, ChaosVerdict: "Pass"}
    if err := p.Publish(context.Background(), "wf-xyz", r); err != nil { t.Fatal(err) }
    cm, err := cs.CoreV1().ConfigMaps("dlh-test-fw").Get(context.Background(), "dlh-result-wf-xyz", metav1.GetOptions{})
    if err != nil { t.Fatal(err) }
    var got map[string]any
    if err := json.Unmarshal([]byte(cm.Data["result.json"]), &got); err != nil { t.Fatal(err) }
    if got["overall"] != true { t.Errorf("overall=%v", got["overall"]) }
}

func TestPublishUpdatesExistingConfigMap(t *testing.T) {
    cs := fake.NewSimpleClientset()
    p := &Publisher{Cs: cs, Namespace: "dlh-test-fw"}
    _ = p.Publish(context.Background(), "wf-xyz", &eval.Result{Overall: false, ChaosVerdict: "Fail"})
    if err := p.Publish(context.Background(), "wf-xyz", &eval.Result{Overall: true, ChaosVerdict: "Pass"}); err != nil { t.Fatal(err) }
    cm, _ := cs.CoreV1().ConfigMaps("dlh-test-fw").Get(context.Background(), "dlh-result-wf-xyz", metav1.GetOptions{})
    var got map[string]any
    _ = json.Unmarshal([]byte(cm.Data["result.json"]), &got)
    if got["overall"] != true { t.Errorf("update didn't take: %v", got) }
}
```

- [ ] **Step 2: Implement `internal/publish/publish.go`**

```go
// Package publish writes the verdict summary to a Kubernetes ConfigMap.
package publish

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
    apierrors "k8s.io/apimachinery/pkg/api/errors"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

type Publisher struct {
    Cs        kubernetes.Interface
    Namespace string
}

func (p *Publisher) Publish(ctx context.Context, workflow string, r *eval.Result) error {
    name := "dlh-result-" + workflow
    body, err := json.Marshal(r)
    if err != nil { return err }
    cm := &corev1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.Namespace,
            Labels: map[string]string{"app.kubernetes.io/managed-by": "dlh-verdict", "dlh.workflow": workflow}},
        Data: map[string]string{"result.json": string(body)},
    }
    _, err = p.Cs.CoreV1().ConfigMaps(p.Namespace).Create(ctx, cm, metav1.CreateOptions{})
    if err == nil { return nil }
    if !apierrors.IsAlreadyExists(err) {
        return fmt.Errorf("publish: create: %w", err)
    }
    _, err = p.Cs.CoreV1().ConfigMaps(p.Namespace).Update(ctx, cm, metav1.UpdateOptions{})
    if err != nil { return fmt.Errorf("publish: update: %w", err) }
    return nil
}
```

- [ ] **Step 3: Run tests**

```bash
go mod tidy
go test ./internal/publish/... -v
```

Expected: both pass.

- [ ] **Step 4: Commit**

```bash
git add verdict-job/internal/publish verdict-job/go.mod verdict-job/go.sum
git commit -m "verdict(publish): patch dlh-result-<workflow> ConfigMap"
```

---

## Task 9: CLI wiring (`cmd/verdict/main.go`)

**Files:**
- Modify: `verdict-job/cmd/verdict/main.go`

- [ ] **Step 1: Replace the stub with the real wiring**

```go
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
    sloPath          string
    chaosResultName  string
    loadStartTS      string
    chaosStartAfter  time.Duration
    chaosDuration    time.Duration
    loadDuration     time.Duration
    promURL          string
    workflowName     string
    artifactDir      string
    namespace        string
    grafanaURL       string
    argoURL          string
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
    if err != nil { log.Fatalf("parse load-start-ts: %v", err) }
    return t
}

func main() {
    f := parseFlags()
    ctx := context.Background()

    sloBytes, err := os.ReadFile(f.sloPath)
    if err != nil { log.Fatalf("read SLO: %v", err) }
    s, err := slo.Parse(sloBytes)
    if err != nil { log.Fatalf("parse SLO: %v", err) }

    win := window.Params{
        LoadStart:       mustParseTime(f.loadStartTS),
        ChaosStartAfter: f.chaosStartAfter,
        ChaosDuration:   f.chaosDuration,
        LoadDuration:    f.loadDuration,
    }
    if err := win.Validate(); err != nil { log.Fatalf("window: %v", err) }

    cfg, err := rest.InClusterConfig()
    if err != nil { log.Fatalf("k8s in-cluster config: %v", err) }
    dyn, _ := dynamic.NewForConfig(cfg)
    cs, _ := kubernetes.NewForConfig(cfg)

    crClient := &chaosresult.Client{
        Dyn: dyn, Namespace: f.namespace,
        GVR: schema.GroupVersionResource{Group: "litmuschaos.io", Version: "v1alpha1", Resource: "chaosresults"},
    }
    chaosV, err := crClient.GetVerdict(ctx, f.chaosResultName, f.chaosVerdictTimeout)
    if err != nil { log.Fatalf("chaos verdict: %v", err) }

    p := prom.New(f.promURL)
    r, err := eval.Evaluate(ctx, s, p, win, chaosV)
    if err != nil { log.Fatalf("eval: %v", err) }

    view := report.View{
        Result: r,
        ScenarioName:    f.workflowName,
        LoadDurationSec: int(f.loadDuration.Seconds()),
        GrafanaURL:      f.grafanaURL,
        ArgoURL:         f.argoURL,
        JSONURL:         "report.json",
    }
    jpath, hpath, err := report.Write(f.artifactDir, view)
    if err != nil { log.Fatalf("report: %v", err) }
    fmt.Printf("wrote %s and %s\n", jpath, hpath)

    pub := &publish.Publisher{Cs: cs, Namespace: f.namespace}
    if err := pub.Publish(ctx, f.workflowName, r); err != nil { log.Fatalf("publish: %v", err) }

    if r.Overall {
        fmt.Println("VERDICT: PASS")
        os.Exit(0)
    }
    fmt.Println("VERDICT: FAIL")
    os.Exit(1)
}
```

- [ ] **Step 2: Build**

```bash
cd /Users/allen/repo/dlh-test-fw/verdict-job
make build
```

Expected: `bin/verdict` produced; no errors.

- [ ] **Step 3: Run with `-h` smoke check**

```bash
./bin/verdict -h 2>&1 | head -30
```

Expected: flag listing prints.

- [ ] **Step 4: Commit**

```bash
git add verdict-job/cmd/verdict/main.go
git commit -m "verdict(cmd): wire CLI with flags, k8s clients, exit code"
```

---

## Task 10: Dockerfile + minikube image load

**Files:**
- Create: `verdict-job/Dockerfile`
- Modify: `verdict-job/Makefile` (already has `image` and `load-image` targets — confirm)

- [ ] **Step 1: Write `Dockerfile`**

```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/verdict ./cmd/verdict

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/verdict /verdict
USER nonroot:nonroot
ENTRYPOINT ["/verdict"]
```

- [ ] **Step 2: Build image**

```bash
cd /Users/allen/repo/dlh-test-fw/verdict-job
make image
```

Expected: `docker images | grep dlh-verdict` shows `0.1.0` tag.

- [ ] **Step 3: Load into minikube**

```bash
make load-image
```

Expected: `minikube image ls | grep dlh-verdict` shows the tag.

- [ ] **Step 4: Update chart values to match (already set to `0.1.0` in Plan 2; verify)**

```bash
grep -A1 'verdict:' /Users/allen/repo/dlh-test-fw/helm/dlh-test-fw/values.yaml
```

Confirm `tag: 0.1.0`. If different, fix and re-commit the chart.

- [ ] **Step 5: Commit**

```bash
cd /Users/allen/repo/dlh-test-fw
git add verdict-job/Dockerfile
git commit -m "verdict: distroless Dockerfile + minikube load target"
```

---

## Task 11: End-to-end test against the running platform

This is an integration check, not a unit test. It runs against the platform stood up in Plan 2.

- [ ] **Step 1: Pre-populate VM with a fake metric**

```bash
kubectl -n dlh-test-fw port-forward svc/dlh-victoria-metrics-single-server 8428:8428 &
PF=$!
sleep 2
for i in 1 2 3; do
  curl -s -X POST 'http://127.0.0.1:8428/api/v1/import/prometheus' \
    --data-binary "fake_lat{scenario=\"e2e\"} 0.2 $(date +%s)000"
done
```

- [ ] **Step 2: Write a tiny SLO file**

```bash
cat > /tmp/slo-e2e.yaml <<'EOF'
thresholds:
- metric: lat
  query: fake_lat{scenario="e2e"}
  lt: 0.5
  window: chaos
EOF
```

- [ ] **Step 3: Fake a ChaosResult (since no real chaos ran)**

```bash
kubectl -n dlh-test-fw apply -f - <<'EOF'
apiVersion: litmuschaos.io/v1alpha1
kind: ChaosResult
metadata: { name: fake-cr }
status:
  experimentStatus: { verdict: Pass }
EOF
```

- [ ] **Step 4: Run verdict from outside the cluster (using `out-of-cluster` config)**

For this manual smoke we substitute `rest.InClusterConfig()` with a kubeconfig path — temporarily edit `main.go` to fall back to `clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))` if in-cluster fails, **only for this step**. Or, equivalently, just run verdict inside the cluster via:

```bash
kubectl -n dlh-test-fw run verdict-smoke --rm -it --restart=Never \
  --image=dlh-verdict:0.1.0 --image-pull-policy=Never \
  --overrides='{"spec":{"serviceAccountName":"argo-workflow"}}' \
  -- \
  -slo-yaml=/tmp/slo-e2e.yaml \
  -chaos-result-name=fake-cr \
  -load-start-ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -chaos-start-after=0s -chaos-duration=10s -load-duration=30s \
  -workflow-name=smoke-1 \
  -artifact-dir=/tmp/verdict
```

The `/tmp/slo-e2e.yaml` won't exist inside the pod — for this smoke run, switch to a ConfigMap-mounted file, or just confirm via a `go run` from outside the cluster against a port-forwarded VM.

**Pragmatic alternative:** `go run ./cmd/verdict ...` from your laptop with KUBECONFIG set + the in-cluster fallback edit above. Treat this step as best-effort; the real end-to-end happens in Plan 5 when scenarios run.

- [ ] **Step 5: Confirm artifacts**

After the run, verify (whichever path you took):
- exit code 0
- `report.json` parses and shows `"overall": true`
- `report.html` opens in a browser and shows the green PASS banner
- `kubectl -n dlh-test-fw get cm dlh-result-smoke-1 -o yaml` shows `result.json` with `"overall":true`

- [ ] **Step 6: Revert any temporary kubeconfig fallback edit. Commit nothing if no code changed.**

---

## Definition of Done

- [ ] `cd verdict-job && go test ./...` is green.
- [ ] `make image && make load-image` succeeds; `minikube image ls` shows `dlh-verdict:0.1.0`.
- [ ] HTML golden file renders cleanly in a browser.
- [ ] Manual smoke (Task 11) produces a green report + populates the ConfigMap.
- [ ] Plan 4 can reference `dlh-verdict:0.1.0` in its `verdict/slo-eval` WorkflowTemplate and know what flags to pass (documented in `main.go`'s flag block).

---

## Self-Review Notes

- **Spec coverage:** "SLO Verdict 設計 → Verdict job 邏輯" steps 1-9 mapped: SLO parse (Task 2), windows (Task 3), threshold + raw_promql eval (Task 6), ChaosResult bounded retry (Task 5), report.json + report.html (Task 7), ConfigMap publish (Task 8), exit code (Task 9). "Report 內容" structure (banner, table, buttons) implemented in `template.html.tmpl`.
- **Placeholders:** None. Every step has working code.
- **Type consistency:** `eval.Result` struct used identically by `report.View` (embedded), `publish.Publisher` (marshalled to JSON), and main.go (built and consumed). `slo.Window` type referenced in `slo`, `window`, `eval` consistently as a `string` enum. `chaosresult.Client.GetVerdict` returns `string` and main.go passes that string directly to `eval.Evaluate(... chaosVerdict string)`. ✓
- **Flag/env naming:** All CLI flags in main.go use kebab-case (`-chaos-result-name`) — Plan 4's WorkflowTemplate must pass identical names. Documented at the top of `cmd/verdict/main.go`.
