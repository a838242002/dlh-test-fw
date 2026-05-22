# dlh-controlplane Phase D (Remote Targets) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add multi-cluster chaos support — the controlplane learns about external "Target" clusters from an Argo-CD-synced ConfigMap + per-target kubeconfig Secrets, routes `/internal/chaos` traffic to the right target via a new `RemoteChaosClient`, surfaces `/api/targets` + a UI Targets page, and populates `Run.Target` end-to-end. The chaos WT `http` template gains a `targetID` param that flows through the entire stack.

**Architecture:** Phase C's `chaos.Client` interface already abstracts chaos injection; Phase D adds `RemoteChaosClient` (per-target dynamic client built from a kubeconfig Secret) and a `Router` that picks `LocalChaosClient` vs `RemoteChaosClient` per call based on a `targetID` arg. A new `targets` package loads target definitions from a ConfigMap + Secret pair (Argo-CD-synced) with a hot-reload watcher. The Workflow CR gains a `dlh.target` label + `targetID` workflow argument that flows into `http` steps' query string. Manifests gain a `runs/index/by-target/{targetID}/...` prefix. UI gets a read-only Targets page + a target dropdown in the scenario submission form.

**Tech Stack:** Go 1.26 (existing module); `k8s.io/client-go/tools/clientcmd` for parsing remote kubeconfigs; dynamic + informer factories built per target. No new external services. kind or a second minikube profile for the e2e smoke.

**Reference spec:** `docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md` (§5 domain model — Target definition; §8 cross-cluster chaos model — Pattern 1 chosen; §9.3 controlplane→target k8s API auth — kubeconfig Secret per target with `chaos-mesh.org/*` + `core/pods get/list` scope; §12 Phase D).

**Branch & worktree:** Per `CLAUDE.md`, work on `feat/plan17-controlplane-remote-targets` in worktree `../dlh-test-fw-plan17`. Task 1 creates it.

**Plan-time decisions / deviations from spec:**

1. **`targetID` is a query param to `/internal/chaos`** (matching Phase C's `runID` convention). Spec §11 implied a body field; query param is cheaper to surface in the WT `http` template's URL.
2. **Target metadata lives in two k8s resources** — a `dlh-targets` ConfigMap with the catalog (id, kubeconfigSecret name, allowedTargetTypes) and per-target Secrets (each holding a single `kubeconfig` key). The split avoids stuffing kubeconfigs into a ConfigMap. Both resources are Argo-CD-synced; the chart ships a placeholder ConfigMap (no entries) so a fresh install has no targets registered. Registration is PR-only (spec §10 UI explicitly says targets are read-only in the UI).
3. **No new Argo CD Application for target-cluster chaos-mesh in this plan.** The spec called for one, but plan-time decision: provide an `argocd/apps/dlh-target-chaos-mesh.yaml.example` template (with `REPLACE-CLUSTER` placeholder) that operators copy + register per target. Auto-deploying chaos-mesh to remote clusters from the framework Argo CD requires cross-cluster Argo CD destinations, which is an Argo CD setup decision, not a controlplane code concern. Documentation tells operators how.
4. **`LocalChaosClient` stays the default** when `targetID` is empty — preserves Phase C local-only behavior so scenarios that don't set `targetID` continue to work unchanged.
5. **No per-target Workflow informer.** The remote target only sees chaos CRs; Workflows still run in the framework cluster. So the existing Workflow informer + Syncer don't change.
6. **Hot reload of target config: file watch, not a control loop.** A simple 30s-poll goroutine in `targets.Registry` re-reads the ConfigMap + Secrets and atomically swaps the cache. No watch-stream; the cost is bounded (1 LIST per minute) and the operational simplicity is worth it. Reconfiguration latency is ~30s, which is fine for ops PRs.
7. **`RemoteChaosClient` uses fresh kubeconfig load per target, not a long-lived informer factory.** Each Create/Delete builds the dynamic client from the cached `*rest.Config` — Phase D goal is correctness, not throughput. If watchdog calls become a bottleneck, add a per-target informer in Phase E or later.
8. **Watchdog scans all targets** by iterating the registry. ListManaged becomes per-client; the watchdog merges results.
9. **`/api/targets/{id}/test`** test-connection endpoint added (deviation from spec's pure-read story): it makes a no-op `GET` against the remote `chaos-mesh.org/v1alpha1/schedules` API and reports success/failure. Cheap, useful for diagnosis. Admin-only (RBAC).
10. **WorkflowTemplate `http` steps gain a `target_id` parameter** that defaults to `""` (empty → local). Existing scenario YAMLs need updating; the three chaos WTs pass it through. Scenarios that want a remote target add `target_id: <id>` to their arguments.
11. **Natural pause points:** after Task 9 (Section A — targets package + Registry + GET /api/targets), Task 14 (Section B — RemoteChaosClient + Router + watchdog cross-cluster), Task 18 (Section C — Run.Target end-to-end + manifest by-target index), Task 22 (Section D — UI + CLI + WT rewiring).

---

## File Structure

**New files (Go backend):**
- `controlplane/internal/targets/registry.go` — `Registry` + `Target` types + ConfigMap/Secret loader + 30s refresh goroutine.
- `controlplane/internal/targets/registry_test.go`
- `controlplane/internal/targets/probe.go` — `Probe(ctx, target)` performs a no-op GET against remote chaos-mesh API.
- `controlplane/internal/chaos/remote.go` — `RemoteChaosClient` impl.
- `controlplane/internal/chaos/remote_test.go`
- `controlplane/internal/chaos/router.go` — `Router` chooses Local vs Remote per call.
- `controlplane/internal/chaos/router_test.go`
- `controlplane/internal/api/targets.go` — `ListTargets`, `GetTarget`, `TestTargetConnection` handlers.

**New files (UI):**
- `controlplane/web/src/pages/TargetsPage.tsx` — read-only list + test-connection button.
- `controlplane/web/src/components/TargetPicker.tsx` — dropdown shown on Scenarios page submit form.

**New files (Argo CD / Helm):**
- `helm/dlh-test-fw/templates/dlh-targets-configmap.yaml` — empty default `dlh-targets` ConfigMap.
- `argocd/apps/dlh-target-chaos-mesh.yaml.example` — operator template for per-target chaos-mesh installs.

**New files (Docs / k8s manifests):**
- `controlplane/deploy/targets-rbac.yaml` — guidance for the SA-on-target-cluster Role (deployed by the operator, not the framework chart).
- `docs/operations/register-target.md` — how to register a new target cluster.

**Modified files:**
- `controlplane/api/openapi.yaml` — add CreateRunRequest.targetId, Run.target, /api/targets/*, /api/targets/{id}/test.
- `controlplane/internal/api/gen/*.gen.go` — regenerated.
- `controlplane/internal/api/handlers.go` — pass through targetID; add new handlers; populate Run.Target.
- `controlplane/internal/api/server.go` — Deps gains `*targets.Registry`; mount new routes.
- `controlplane/internal/api/internal_chaos.go` — read `targetID` query param; pass to chaos router.
- `controlplane/internal/runs/submit.go` — Workflow gets a `dlh.target` label + `target_id` argument forwarded as workflow.arguments.
- `controlplane/internal/runs/manifest.go` — add by-target index write path.
- `controlplane/internal/runs/syncer.go` — propagate `Target` field into manifest.
- `controlplane/internal/chaos/watchdog.go` — iterate over Router (all targets) instead of single client.
- `controlplane/internal/auth/rbac.go` — add `RequireAdmin` check for /api/targets/{id}/test.
- `controlplane/cmd/dlh-controlplane/main.go` — construct Registry; build Router from registry; wire watchdog.
- `controlplane/cmd/dlh/runs.go` — `dlh run --target` flag.
- `controlplane/cmd/dlh/run.go` — same; threads through to body.
- `controlplane/web/src/App.tsx` — add /targets route + nav link.
- `controlplane/web/src/pages/ScenariosPage.tsx` — wire `TargetPicker` into submit form.
- `controlplane/web/src/pages/RunsPage.tsx` + `RunDetailPage.tsx` — show Target column / field.
- `helm/dlh-test-fw/files/workflowtemplates/chaos/{pod-delete,network-loss,kafka-broker-partition}.yaml` — add `target_id` parameter forwarded to `/internal/chaos?targetID=…`.
- `controlplane/deploy/role.yaml` — extend with `core/secrets get,list,watch` on `dlh-target-*` resourceName pattern (a wildcard-by-prefix isn't supported by RBAC, so list secrets in the dlh namespace then filter client-side — see Task 4 note).
- `controlplane/deploy/deployment.yaml` — no env-var changes; the registry watches in-cluster Secrets.
- `.github/workflows/ci.yml` — extend smoke / kubeconform paths.
- `CLAUDE.md`, `docs/FINDINGS.md`, `README.md` — Plan 17 notes.

**Unchanged:** Phase B GET endpoints, OIDC verifier, MinIO ReportReader, Dockerfile, Workflow informer.

---

## Task 1: Baseline + worktree

No commits.

- [ ] **Step 1: Verify clean main + CI green + Phase C present.**

```bash
cd /Users/allen/repo/dlh-test-fw
git status
git log --first-parent --oneline -5
gh run list --branch main --limit 1
ls controlplane/internal/chaos/
ls controlplane/internal/runs/
```

Expected: clean tree on `main`; Plan 16 merge `abf407d` visible. The chaos/ dir has `client.go`, `local_test.go`, `watchdog.go`, `watchdog_test.go`. The runs/ dir has Submitter + ManifestWriter + Syncer + tests.

- [ ] **Step 2: Confirm `chaos.Client` interface shape (Phase D extends it).**

```bash
grep -A 8 "^type Client interface" controlplane/internal/chaos/client.go
```

Expected: `Create`, `Delete`, `DeleteByRun`, `ListByRun`, `ListManaged` methods, all `ctx context.Context` first.

- [ ] **Step 3: Create the feature worktree using the ABSOLUTE path** (Plan 15 had a cwd issue; the explicit absolute path avoids it).

```bash
cd /Users/allen/repo/dlh-test-fw
git worktree add /Users/allen/repo/dlh-test-fw-plan17 -b feat/plan17-controlplane-remote-targets main
cd /Users/allen/repo/dlh-test-fw-plan17
git worktree list
git status
```

Expected: clean tree on `feat/plan17-controlplane-remote-targets`; `git worktree list` shows `/Users/allen/repo/dlh-test-fw-plan17` as a sibling.

- [ ] **Step 4: Verify the Phase C baseline builds + tests pass.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17/controlplane
make ui-build 2>&1 | tail -3
go build ./...
go test ./...
```

Expected: clean ui-build; all Phase C tests pass.

All remaining tasks run from `/Users/allen/repo/dlh-test-fw-plan17`.

---

# Section A — Targets package + Registry + GET /api/targets (Tasks 2-9)

## Task 2: Targets package skeleton + Target type

**Files:**
- Create: `controlplane/internal/targets/registry.go`
- Create: `controlplane/internal/targets/registry_test.go`

- [ ] **Step 1: Write `internal/targets/registry.go`** with just the type definitions:

```go
// Package targets owns the registry of remote target clusters that the
// controlplane can inject chaos into. Definitions are loaded from a
// Kubernetes ConfigMap (dlh-targets) + per-target kubeconfig Secrets,
// both Argo-CD-synced. The registry refreshes every 30s by re-reading
// both resources and atomically swapping the cache.
package targets

import (
	"sync"
	"time"

	"k8s.io/client-go/rest"
)

// Target describes one remote cluster the controlplane can talk to.
type Target struct {
	// ID is the user-facing identifier (e.g. "staging-mysql"). Stable.
	ID string `yaml:"id"`
	// DisplayName is human-readable. Defaults to ID if empty.
	DisplayName string `yaml:"displayName,omitempty"`
	// KubeconfigSecret names the Secret holding the kubeconfig (key: kubeconfig).
	KubeconfigSecret string `yaml:"kubeconfigSecret"`
	// AllowedTargetTypes filters which scenarios can target this cluster.
	// Empty list = no filter (any scenario allowed).
	AllowedTargetTypes []string `yaml:"allowedTargetTypes,omitempty"`
	// Namespace is the chaos namespace on the remote cluster. Defaults to "dlh-test-fw".
	Namespace string `yaml:"namespace,omitempty"`
}

// LoadedTarget is a Target plus its parsed *rest.Config (kubeconfig).
type LoadedTarget struct {
	Target
	RestConfig *rest.Config
	LastSeen   time.Time
}

// Registry holds the current set of loaded targets, refreshed periodically.
type Registry struct {
	mu      sync.RWMutex
	loaded  map[string]*LoadedTarget // id -> LoadedTarget
}

// NewRegistry returns an empty registry. Call Refresh() to populate.
func NewRegistry() *Registry {
	return &Registry{loaded: map[string]*LoadedTarget{}}
}

// Get returns the LoadedTarget for an id, or nil if not registered.
func (r *Registry) Get(id string) *LoadedTarget {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loaded[id]
}

// List returns a snapshot of all loaded targets.
func (r *Registry) List() []*LoadedTarget {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*LoadedTarget, 0, len(r.loaded))
	for _, t := range r.loaded {
		out = append(out, t)
	}
	return out
}

// Replace atomically swaps the loaded map (used by Refresh).
func (r *Registry) Replace(targets map[string]*LoadedTarget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded = targets
}
```

- [ ] **Step 2: Write `internal/targets/registry_test.go`:**

```go
package targets

import "testing"

func TestRegistry_GetAndList(t *testing.T) {
	r := NewRegistry()
	if r.Get("nope") != nil {
		t.Errorf("Get on empty registry should be nil")
	}
	r.Replace(map[string]*LoadedTarget{
		"a": {Target: Target{ID: "a"}},
		"b": {Target: Target{ID: "b"}},
	})
	if r.Get("a") == nil || r.Get("b") == nil {
		t.Errorf("Get missed populated targets")
	}
	if r.Get("c") != nil {
		t.Errorf("Get on unknown id should be nil")
	}
	if len(r.List()) != 2 {
		t.Errorf("List length: %d", len(r.List()))
	}
}
```

- [ ] **Step 3: Run tests.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17/controlplane
go build ./...
go test ./internal/targets/...
```

Expected: 1 test PASS.

- [ ] **Step 4: Commit.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17
git add controlplane/internal/targets/
git commit -m "feat(controlplane/targets): Registry + Target type skeleton

Concurrent-safe Get/List/Replace primitives. ConfigMap+Secret loader
lands in Task 3."
```

---

## Task 3: ConfigMap + Secret loader

**Files:**
- Modify: `controlplane/internal/targets/registry.go`
- Modify: `controlplane/internal/targets/registry_test.go`

- [ ] **Step 1: Append the loader to `internal/targets/registry.go`.**

```go
import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
)

// Loader fetches the dlh-targets ConfigMap and per-target Secrets and
// builds a fresh LoadedTarget map.
type Loader struct {
	Client    kubernetes.Interface
	Namespace string
	// ConfigMapName defaults to "dlh-targets".
	ConfigMapName string
}

// Load reads the configmap + secrets and returns the current target set.
func (l *Loader) Load(ctx context.Context) (map[string]*LoadedTarget, error) {
	cmName := l.ConfigMapName
	if cmName == "" {
		cmName = "dlh-targets"
	}
	cm, err := l.Client.CoreV1().ConfigMaps(l.Namespace).Get(ctx, cmName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get configmap %s/%s: %w", l.Namespace, cmName, err)
	}
	rawYAML, ok := cm.Data["targets.yaml"]
	if !ok {
		// Empty registry — chart ships an empty default, this is fine.
		return map[string]*LoadedTarget{}, nil
	}
	var doc struct {
		Targets []Target `yaml:"targets"`
	}
	if err := yaml.Unmarshal([]byte(rawYAML), &doc); err != nil {
		return nil, fmt.Errorf("parse targets.yaml: %w", err)
	}
	out := map[string]*LoadedTarget{}
	for i := range doc.Targets {
		t := doc.Targets[i]
		if t.ID == "" || t.KubeconfigSecret == "" {
			continue // skip malformed entries
		}
		if t.Namespace == "" {
			t.Namespace = "dlh-test-fw"
		}
		if t.DisplayName == "" {
			t.DisplayName = t.ID
		}
		cfg, err := l.loadKubeconfig(ctx, t.KubeconfigSecret)
		if err != nil {
			// Don't fail the whole load — surface partial results so a single
			// broken secret doesn't disable the registry.
			out[t.ID] = &LoadedTarget{Target: t, LastSeen: metav1.Now().Time}
			out[t.ID].RestConfig = nil
			continue
		}
		out[t.ID] = &LoadedTarget{Target: t, RestConfig: cfg, LastSeen: metav1.Now().Time}
	}
	return out, nil
}

func (l *Loader) loadKubeconfig(ctx context.Context, secretName string) (*corev1.ConfigMap, error) {
	return nil, fmt.Errorf("kubeconfig loading: not implemented; replaced below")
}

// (Helper rewrite — proper signature returning *rest.Config.)
```

The helper needs the correct signature returning `*rest.Config`. Replace the stub with:

```go
func (l *Loader) loadKubeconfig(ctx context.Context, secretName string) (*rest.Config, error) {
	sec, err := l.Client.CoreV1().Secrets(l.Namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", l.Namespace, secretName, err)
	}
	raw, ok := sec.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("secret %s missing 'kubeconfig' key", secretName)
	}
	cfg, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	return cfg, nil
}
```

(Delete the stub. The single working signature returns `*rest.Config`.)

The `sigs.k8s.io/yaml` import drops in a transitive — it's already in client-go's graph. Run `go mod tidy`.

- [ ] **Step 2: Add tests using fake k8s clientset.**

Append to `internal/targets/registry_test.go`:

```go
import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLoader_EmptyConfigMap(t *testing.T) {
	ns := "dlh-test-fw"
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "dlh-targets", Namespace: ns},
		Data:       map[string]string{},
	}
	client := fake.NewSimpleClientset(cm)
	l := &Loader{Client: client, Namespace: ns}
	got, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty registry, got %d entries", len(got))
	}
}

func TestLoader_TargetEntries(t *testing.T) {
	ns := "dlh-test-fw"
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "dlh-targets", Namespace: ns},
		Data: map[string]string{
			"targets.yaml": `
targets:
  - id: staging-mysql
    kubeconfigSecret: dlh-target-staging-mysql
    allowedTargetTypes: [mysql]
    namespace: dlh-test-fw
  - id: preprod-kafka
    kubeconfigSecret: dlh-target-preprod-kafka
    allowedTargetTypes: [kafka]
`,
		},
	}
	// Provide a minimal valid kubeconfig in each target secret. Loader
	// tolerates a missing secret (LoadedTarget present with nil RestConfig),
	// so we can verify both with-and-without paths.
	validKubeconfig := []byte(`apiVersion: v1
kind: Config
clusters:
  - name: t
    cluster:
      server: https://example.com
contexts:
  - name: t
    context:
      cluster: t
      user: t
current-context: t
users:
  - name: t
    user: {}`)
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "dlh-target-staging-mysql", Namespace: ns},
		Data:       map[string][]byte{"kubeconfig": validKubeconfig},
	}
	client := fake.NewSimpleClientset(cm, sec)
	l := &Loader{Client: client, Namespace: ns}

	got, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 targets, got %d", len(got))
	}
	if got["staging-mysql"] == nil || got["staging-mysql"].RestConfig == nil {
		t.Errorf("staging-mysql should have RestConfig: %+v", got["staging-mysql"])
	}
	// preprod-kafka has no secret in the fake client → tolerated, RestConfig nil
	if got["preprod-kafka"] == nil || got["preprod-kafka"].RestConfig != nil {
		t.Errorf("preprod-kafka should be present with nil RestConfig: %+v", got["preprod-kafka"])
	}
	if got["staging-mysql"].DisplayName != "staging-mysql" {
		t.Errorf("DisplayName default: %q", got["staging-mysql"].DisplayName)
	}
}
```

- [ ] **Step 3: Build + test.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17/controlplane
go mod tidy
go build ./...
go test ./internal/targets/... -v
```

Expected: 3 tests PASS. If `sigs.k8s.io/yaml` doesn't resolve, run `go get sigs.k8s.io/yaml@v1.4.0` then retry.

- [ ] **Step 4: Commit.**

```bash
git add controlplane/internal/targets/ controlplane/go.mod controlplane/go.sum
git commit -m "feat(controlplane/targets): ConfigMap+Secret loader

Loads the dlh-targets ConfigMap (targets.yaml under .data); for each
entry, resolves the named Secret's kubeconfig key into a *rest.Config.
Tolerates missing or malformed secrets (LoadedTarget without RestConfig
remains visible to the API for diagnosis)."
```

---

## Task 4: Registry refresh loop

**Files:**
- Modify: `controlplane/internal/targets/registry.go`
- Modify: `controlplane/internal/targets/registry_test.go`

- [ ] **Step 1: Add `Run` method to Loader that periodically refreshes the Registry.**

Append to `internal/targets/registry.go`:

```go
import "log/slog"

// Run repeatedly Loads + Replaces into the registry every interval until
// ctx is cancelled. First refresh runs synchronously so the registry is
// populated before Run returns control to its goroutine.
type Refresher struct {
	Loader   *Loader
	Registry *Registry
	Interval time.Duration // defaults to 30s
}

func (r *Refresher) Run(ctx context.Context) {
	interval := r.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}
	// First tick immediately.
	r.tick(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.tick(ctx)
		}
	}
}

func (r *Refresher) tick(ctx context.Context) {
	loaded, err := r.Loader.Load(ctx)
	if err != nil {
		slog.Warn("targets refresh failed", "err", err)
		return
	}
	r.Registry.Replace(loaded)
}
```

- [ ] **Step 2: Add a test for Refresher.tick.**

Append to `internal/targets/registry_test.go`:

```go
func TestRefresher_TickPopulatesRegistry(t *testing.T) {
	ns := "dlh-test-fw"
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "dlh-targets", Namespace: ns},
		Data: map[string]string{
			"targets.yaml": `
targets:
  - id: a
    kubeconfigSecret: missing-secret
`,
		},
	}
	client := fake.NewSimpleClientset(cm)
	reg := NewRegistry()
	rf := &Refresher{
		Loader:   &Loader{Client: client, Namespace: ns},
		Registry: reg,
		Interval: 10 * time.Millisecond,
	}
	rf.tick(context.Background())
	if reg.Get("a") == nil {
		t.Errorf("registry not populated after tick")
	}
}
```

- [ ] **Step 3: Build + test.**

```bash
go build ./...
go test ./internal/targets/... -v
```

Expected: 4 tests PASS.

- [ ] **Step 4: Commit.**

```bash
git add controlplane/internal/targets/
git commit -m "feat(controlplane/targets): Refresher polls every 30s

Synchronous first tick so the registry is populated before the
goroutine returns control."
```

---

## Task 5: Extend controlplane Role to read dlh-target-* secrets

**Files:**
- Modify: `controlplane/deploy/role.yaml`

- [ ] **Step 1: Read the existing Role.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17
cat controlplane/deploy/role.yaml
```

- [ ] **Step 2: Replace the rules block** to allow listing secrets (RBAC `resourceNames` doesn't support wildcards; the controller must `list` and filter client-side). The chart-managed `dlh-internal-token` Secret is unchanged. We add a broad list+get on secrets in the controlplane's own namespace — the controlplane is the only consumer of that namespace's secrets in production setup.

Wait — that's too broad. Better path: keep `get` scoped to `resourceNames` for the known secrets (internal-token + dlh-targets). Targets the operator creates follow the convention `dlh-target-*`. We can add a rule that allows `list+watch` on the entire namespace's secrets *without get/read individual ones*; then `get` is allowed only on the resourceNames pattern by listing and identifying. But RBAC's `get + resourceNames` semantics require knowing the name up front.

**Cleanest path:** allow `get` on the entire secrets resource in the controlplane namespace. This is the same posture argo-server has. Document in the commit that secrets in the controlplane namespace are limited to platform-managed ones (operator-installed Argo CD enforces that no other workloads inject secrets there).

Use the `Edit` tool. Replace the `rules:` block with:

```yaml
rules:
  - apiGroups: ["argoproj.io"]
    resources: ["workflows"]
    verbs: ["get", "list", "watch", "create", "patch", "delete"]
  - apiGroups: ["argoproj.io"]
    resources: ["workflowtemplates"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch"]
    resourceNames: ["dlh-roles", "dlh-targets"]
  - apiGroups: [""]
    resources: ["secrets"]
    # 'get' on all secrets in the controlplane namespace so we can
    # resolve dlh-target-* kubeconfig secrets (the set is dynamic; RBAC
    # doesn't allow wildcard resourceNames). The framework cluster is
    # responsible for ensuring no untrusted workloads share this
    # namespace. dlh-internal-token is also covered by this rule.
    verbs: ["get", "list", "watch"]
  - apiGroups: ["chaos-mesh.org"]
    resources: ["schedules", "podchaos", "networkchaos"]
    verbs: ["get", "list", "watch", "create", "delete"]
```

- [ ] **Step 3: Validate manifests.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  controlplane/deploy/*.yaml | tail -3
```

Expected: `Valid: 7`, `Invalid: 0`.

- [ ] **Step 4: Commit.**

```bash
git add controlplane/deploy/role.yaml
git commit -m "feat(controlplane): Role gets list/watch on secrets + dlh-targets ConfigMap

Needed by the targets Loader to resolve per-target kubeconfig Secrets.
RBAC resourceNames can't wildcard-match dlh-target-*; we authorise get
on the controlplane namespace's secrets generally and document that
the namespace is platform-managed (no untrusted workload secrets)."
```

---

## Task 6: Empty default `dlh-targets` ConfigMap

**Files:**
- Create: `helm/dlh-test-fw/templates/dlh-targets-configmap.yaml`

- [ ] **Step 1: Write the template.**

```yaml
# Placeholder catalog of remote target clusters. Operators register a
# target by adding an entry under .data.targets.yaml and committing a
# matching dlh-target-<id> Secret (key: kubeconfig). Both are
# Argo-CD-synced. See docs/operations/register-target.md.
#
# On a fresh install this ConfigMap is empty (no entries); the
# controlplane behaves exactly like Phase C when the registry is empty.
apiVersion: v1
kind: ConfigMap
metadata:
  name: dlh-targets
  namespace: {{ .Values.namespace }}
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
data:
  targets.yaml: |
    targets: []
```

- [ ] **Step 2: Render + validate.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17
helm dependency update helm/dlh-test-fw 2>&1 | tail -3
helm template dlh helm/dlh-test-fw > /tmp/rendered-plan17.yaml
grep -A 3 'name: dlh-targets' /tmp/rendered-plan17.yaml | head -10
```

Expected: ConfigMap rendered with `targets: []`.

- [ ] **Step 3: Commit.**

```bash
git add helm/dlh-test-fw/templates/dlh-targets-configmap.yaml
git commit -m "feat(chart): empty default dlh-targets ConfigMap

Operators populate targets.yaml via PR. Empty default means a fresh
install is fully Phase-C-compatible: registry empty, LocalChaosClient
is the only impl in use."
```

---

## Task 7: Probe (test-connection) for targets

**Files:**
- Create: `controlplane/internal/targets/probe.go`
- Create: `controlplane/internal/targets/probe_test.go`

- [ ] **Step 1: Write `internal/targets/probe.go`.**

```go
package targets

import (
	"context"
	"errors"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ProbeResult is what the /api/targets/{id}/test endpoint returns.
type ProbeResult struct {
	OK        bool          `json:"ok"`
	Latency   time.Duration `json:"latencyNanos"`
	Error     string        `json:"error,omitempty"`
}

// Probe makes a list-with-limit=1 call against chaos-mesh.org/v1alpha1
// schedules. Confirms (a) the kubeconfig parses, (b) the cluster API is
// reachable, (c) the SA can read chaos resources. Cheap.
func Probe(ctx context.Context, t *LoadedTarget) ProbeResult {
	if t == nil || t.RestConfig == nil {
		return ProbeResult{OK: false, Error: "target has no kubeconfig"}
	}
	dyn, err := dynamic.NewForConfig(t.RestConfig)
	if err != nil {
		return ProbeResult{OK: false, Error: "dynamic client: " + err.Error()}
	}
	gvr := schema.GroupVersionResource{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "schedules"}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	_, err = dyn.Resource(gvr).Namespace(t.Namespace).List(probeCtx, metav1.ListOptions{Limit: 1})
	elapsed := time.Since(start)
	if err != nil {
		return ProbeResult{OK: false, Latency: elapsed, Error: err.Error()}
	}
	return ProbeResult{OK: true, Latency: elapsed}
}

// guard against accidentally unused import
var _ = errors.New
```

- [ ] **Step 2: Write a thin test.** The Probe makes a real cluster call which we can't fake here — keep the test scope narrow: nil-target returns OK=false, no kubeconfig returns OK=false.

```go
package targets

import (
	"context"
	"testing"
)

func TestProbe_NilTarget(t *testing.T) {
	res := Probe(context.Background(), nil)
	if res.OK {
		t.Errorf("nil target should return OK=false")
	}
}

func TestProbe_NoKubeconfig(t *testing.T) {
	res := Probe(context.Background(), &LoadedTarget{Target: Target{ID: "x"}})
	if res.OK {
		t.Errorf("missing kubeconfig should return OK=false")
	}
	if res.Error == "" {
		t.Errorf("expected error message, got empty")
	}
}
```

- [ ] **Step 3: Build + test.**

```bash
go build ./...
go test ./internal/targets/... -v
```

Expected: 6 tests pass.

- [ ] **Step 4: Commit.**

```bash
git add controlplane/internal/targets/probe.go controlplane/internal/targets/probe_test.go
git commit -m "feat(controlplane/targets): Probe makes a cheap list-1 call against remote chaos-mesh API

Surfaces: kubeconfig validity + API reachability + RBAC. Used by the
/api/targets/{id}/test endpoint in Task 9."
```

---

## Task 8: OpenAPI for /api/targets + Run.Target

**Files:**
- Modify: `controlplane/api/openapi.yaml`
- Regenerate: `controlplane/internal/api/gen/*.gen.go`, `controlplane/web/src/api/gen.ts`

- [ ] **Step 1: Add Target schemas and endpoints.**

Use the `Edit` tool. Insert after the existing `/internal/chaos/{ref}:` path (before `components:` block):

```yaml
  /api/targets:
    get:
      operationId: listTargets
      responses:
        "200":
          description: registered targets
          content:
            application/json:
              schema:
                type: object
                required: [items]
                properties:
                  items:
                    type: array
                    items: { $ref: "#/components/schemas/Target" }
  /api/targets/{id}:
    get:
      operationId: getTarget
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      responses:
        "200":
          description: target detail
          content:
            application/json:
              schema: { $ref: "#/components/schemas/Target" }
        "404":
          description: not found
  /api/targets/{id}/test:
    post:
      operationId: testTargetConnection
      parameters:
        - in: path
          name: id
          required: true
          schema: { type: string }
      responses:
        "200":
          description: probe result
          content:
            application/json:
              schema: { $ref: "#/components/schemas/ProbeResult" }
        "404":
          description: not found
```

- [ ] **Step 2: Add `Target` + `ProbeResult` schemas + extend Run / CreateRunRequest.**

In the `components.schemas:` block, after `ChaosResourceRef`, append:

```yaml
    Target:
      type: object
      required: [id]
      properties:
        id:                 { type: string }
        displayName:        { type: string }
        kubeconfigSecret:   { type: string }
        allowedTargetTypes: { type: array, items: { type: string } }
        namespace:          { type: string }
        configured:         { type: boolean, description: "True if a valid kubeconfig was loaded for this target." }
    ProbeResult:
      type: object
      required: [ok]
      properties:
        ok:           { type: boolean }
        latencyNanos: { type: integer, format: int64 }
        error:        { type: string }
```

- [ ] **Step 3: Extend `CreateRunRequest`** with an optional `targetId` field. Find the existing CreateRunRequest schema and replace it:

```yaml
    CreateRunRequest:
      type: object
      required: [scenarioId]
      properties:
        scenarioId:
          type: string
          description: "WorkflowTemplate name (e.g. mysql-pod-delete)"
        targetId:
          type: string
          description: "Optional remote target ID. Empty = inject chaos in framework cluster."
        parameters:
          type: object
          description: "Optional parameter overrides. Keys are WT parameter names."
          additionalProperties:
            type: string
```

- [ ] **Step 4: Extend `Run`** with an optional `target` field. Find Run schema and add:

```yaml
        target:
          type: string
          description: "Remote target ID (Run was injected into a remote cluster). Empty = local."
```

- [ ] **Step 5: Same for `RunDetail`** (it includes Run by allOf in earlier plans but our codegen flattened it). Add `target` to RunDetail's properties block too:

```yaml
        target:
          type: string
```

- [ ] **Step 6: Regenerate.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17/controlplane
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
    -config api/oapi-codegen-server.yaml \
    api/openapi.yaml
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
    -config api/oapi-codegen-types.yaml \
    api/openapi.yaml
cd web && pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts && cd ..
```

- [ ] **Step 7: Add handler stubs.** Append to `internal/api/handlers.go` (real impls in Task 9):

```go
func (h *Handlers) ListTargets(_ context.Context, _ gen.ListTargetsRequestObject) (gen.ListTargetsResponseObject, error) {
	return gen.ListTargets200JSONResponse{Items: []gen.Target{}}, nil
}
func (h *Handlers) GetTarget(_ context.Context, _ gen.GetTargetRequestObject) (gen.GetTargetResponseObject, error) {
	return gen.GetTarget404Response{}, nil
}
func (h *Handlers) TestTargetConnection(_ context.Context, _ gen.TestTargetConnectionRequestObject) (gen.TestTargetConnectionResponseObject, error) {
	return gen.TestTargetConnection404Response{}, nil
}
```

(The exact response-type names match Phase C / Plan 16 — confirm with `grep "^type ListTargets" internal/api/gen/server.gen.go`.)

- [ ] **Step 8: Build.**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 9: Commit.**

```bash
git add controlplane/api/openapi.yaml controlplane/internal/api/gen/ controlplane/internal/api/handlers.go controlplane/web/src/api/gen.ts
git commit -m "feat(controlplane): OpenAPI gains /api/targets + Run.Target + CreateRunRequest.targetId

Adds ListTargets + GetTarget + TestTargetConnection operations. Run and
RunDetail gain an optional target field. CreateRunRequest gains optional
targetId. Handler stubs land; real implementations in Task 9 (targets
endpoints) and Task 15 (CreateRun targetId pass-through)."
```

---

## Task 9: Wire Registry + Targets handlers

**Files:**
- Modify: `controlplane/internal/api/server.go` (Deps gains Registry)
- Modify: `controlplane/internal/api/handlers.go` (real ListTargets / GetTarget / TestTargetConnection)
- Create: `controlplane/internal/api/targets.go` (handler helpers + DTO conversion)
- Create: `controlplane/internal/api/targets_test.go`
- Modify: `controlplane/cmd/dlh-controlplane/main.go` (construct registry + refresher goroutine)

- [ ] **Step 1: Extend Deps in `internal/api/server.go`.** Append the field:

```go
Targets *targets.Registry
```

Import: `"github.com/dlh/dlh-test-fw/controlplane/internal/targets"`.

- [ ] **Step 2: Write `internal/api/targets.go`:**

```go
package api

import (
	"context"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
)

func targetDTO(t *targets.LoadedTarget) gen.Target {
	allowed := append([]string(nil), t.AllowedTargetTypes...)
	displayName := t.DisplayName
	kc := t.KubeconfigSecret
	ns := t.Namespace
	configured := t.RestConfig != nil
	return gen.Target{
		Id:                 t.ID,
		DisplayName:        &displayName,
		KubeconfigSecret:   &kc,
		AllowedTargetTypes: &allowed,
		Namespace:          &ns,
		Configured:         &configured,
	}
}

// Phase D: handler bodies live alongside the existing handlers.go entries
// but the conversion helpers live here to keep handlers.go focused.

var _ = context.TODO // placate the linter if no other use yet
```

- [ ] **Step 3: Replace the Task 8 stubs in `internal/api/handlers.go` with real implementations:**

```go
func (h *Handlers) ListTargets(_ context.Context, _ gen.ListTargetsRequestObject) (gen.ListTargetsResponseObject, error) {
	if h.deps.Targets == nil {
		return gen.ListTargets200JSONResponse{Items: []gen.Target{}}, nil
	}
	loaded := h.deps.Targets.List()
	items := make([]gen.Target, 0, len(loaded))
	for _, t := range loaded {
		items = append(items, targetDTO(t))
	}
	return gen.ListTargets200JSONResponse{Items: items}, nil
}

func (h *Handlers) GetTarget(_ context.Context, req gen.GetTargetRequestObject) (gen.GetTargetResponseObject, error) {
	if h.deps.Targets == nil {
		return gen.GetTarget404Response{}, nil
	}
	t := h.deps.Targets.Get(req.Id)
	if t == nil {
		return gen.GetTarget404Response{}, nil
	}
	return gen.GetTarget200JSONResponse(targetDTO(t)), nil
}

func (h *Handlers) TestTargetConnection(ctx context.Context, req gen.TestTargetConnectionRequestObject) (gen.TestTargetConnectionResponseObject, error) {
	if h.deps.Targets == nil {
		return gen.TestTargetConnection404Response{}, nil
	}
	t := h.deps.Targets.Get(req.Id)
	if t == nil {
		return gen.TestTargetConnection404Response{}, nil
	}
	res := targets.Probe(ctx, t)
	latencyNanos := res.Latency.Nanoseconds()
	errStr := res.Error
	return gen.TestTargetConnection200JSONResponse{
		Ok:           res.OK,
		LatencyNanos: &latencyNanos,
		Error:        &errStr,
	}, nil
}
```

(Confirm the struct field names in `gen.TestTargetConnection200JSONResponse` — they may be lowercased `Ok` or `OK`. Compiler will tell you.)

- [ ] **Step 4: Add a thin handler test.**

`controlplane/internal/api/targets_test.go`:

```go
package api

import (
	"context"
	"testing"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
)

func TestListTargets_EmptyRegistry(t *testing.T) {
	deps := &Deps{Targets: targets.NewRegistry()}
	h := &Handlers{deps: deps}
	resp, err := h.ListTargets(context.Background(), gen.ListTargetsRequestObject{})
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	out, ok := resp.(gen.ListTargets200JSONResponse)
	if !ok {
		t.Fatalf("response type: %T", resp)
	}
	if len(out.Items) != 0 {
		t.Errorf("expected empty, got %d", len(out.Items))
	}
}

func TestGetTarget_404OnUnknown(t *testing.T) {
	deps := &Deps{Targets: targets.NewRegistry()}
	h := &Handlers{deps: deps}
	resp, err := h.GetTarget(context.Background(), gen.GetTargetRequestObject{Id: "nope"})
	if err != nil {
		t.Fatalf("GetTarget: %v", err)
	}
	if _, ok := resp.(gen.GetTarget404Response); !ok {
		t.Fatalf("expected 404, got %T", resp)
	}
}

func TestListTargets_PopulatedRegistry(t *testing.T) {
	r := targets.NewRegistry()
	r.Replace(map[string]*targets.LoadedTarget{
		"staging-mysql": {Target: targets.Target{
			ID: "staging-mysql", DisplayName: "Staging MySQL", AllowedTargetTypes: []string{"mysql"}, KubeconfigSecret: "dlh-target-staging-mysql",
		}},
	})
	deps := &Deps{Targets: r}
	h := &Handlers{deps: deps}
	resp, _ := h.ListTargets(context.Background(), gen.ListTargetsRequestObject{})
	out := resp.(gen.ListTargets200JSONResponse)
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 target, got %d", len(out.Items))
	}
	if out.Items[0].Id != "staging-mysql" {
		t.Errorf("id: %q", out.Items[0].Id)
	}
}
```

- [ ] **Step 5: Modify main.go** to construct the Registry + Refresher and pass it into Deps. Find a good insertion point after the chaos client construction (where Phase C ended):

```go
// Phase D: targets registry.
targetsReg := targets.NewRegistry()
loader := &targets.Loader{Client: clients.Core, Namespace: cfg.K8sNamespace}
refresher := &targets.Refresher{Loader: loader, Registry: targetsReg, Interval: 30 * time.Second}
go refresher.Run(ctx)
```

Add `Targets: targetsReg` to the Deps literal. Add import `"github.com/dlh/dlh-test-fw/controlplane/internal/targets"`.

- [ ] **Step 6: Build + test.**

```bash
go mod tidy
go build ./...
go test ./...
```

Expected: clean build; new targets handler tests pass; existing tests still pass.

- [ ] **Step 7: Commit.**

```bash
git add controlplane/internal/api/server.go controlplane/internal/api/handlers.go \
        controlplane/internal/api/targets.go controlplane/internal/api/targets_test.go \
        controlplane/cmd/dlh-controlplane/main.go controlplane/go.sum
git commit -m "feat(controlplane): wire targets registry + /api/targets handlers

ListTargets / GetTarget / TestTargetConnection backed by the registry.
Refresher goroutine started in main; first refresh runs synchronously.
Test connection makes a real cluster GET — Phase D's first cross-cluster
call goes through the Probe helper."
```

**Section A complete.** Targets registry loads from ConfigMap+Secrets every 30s; `/api/targets` is queryable. No remote chaos yet.

---

# Section B — RemoteChaosClient + Router + cross-cluster watchdog (Tasks 10-14)

## Task 10: RemoteChaosClient skeleton

**Files:**
- Create: `controlplane/internal/chaos/remote.go`
- Create: `controlplane/internal/chaos/remote_test.go`

- [ ] **Step 1: Write `internal/chaos/remote.go`.**

```go
package chaos

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// RemoteChaosClient creates chaos CRs in a target cluster identified by
// the kubeconfig that built RestConfig. Namespace is the chaos namespace
// on the remote cluster.
type RemoteChaosClient struct {
	RestConfig *rest.Config
	Namespace  string
	// TargetID is stored on labels for watchdog reconciliation.
	TargetID string
}

func (r *RemoteChaosClient) dyn() (dynamic.Interface, error) {
	if r.RestConfig == nil {
		return nil, fmt.Errorf("remote chaos client: no kubeconfig (target %q)", r.TargetID)
	}
	return dynamic.NewForConfig(r.RestConfig)
}

func (r *RemoteChaosClient) Create(ctx context.Context, runID string, res Resource) (Ref, error) {
	gvr := gvrFromAPIVersion(res.APIVersion, res.Kind)
	if gvr.Empty() {
		return Ref{}, fmt.Errorf("unsupported chaos kind: %s/%s", res.APIVersion, res.Kind)
	}
	dyn, err := r.dyn()
	if err != nil {
		return Ref{}, err
	}
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": res.APIVersion,
		"kind":       res.Kind,
		"metadata":   res.Metadata,
		"spec":       res.Spec,
	}}
	u.SetNamespace(r.Namespace)
	labels := u.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels["dlh.run-id"] = runID
	labels["dlh.managed-by"] = "controlplane"
	labels["dlh.target"] = r.TargetID
	u.SetLabels(labels)
	created, err := dyn.Resource(gvr).Namespace(r.Namespace).Create(ctx, u, metav1.CreateOptions{})
	if err != nil {
		return Ref{}, fmt.Errorf("create %s on target %s: %w", res.Kind, r.TargetID, err)
	}
	return Ref{
		Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource,
		Namespace: r.Namespace, Name: created.GetName(),
	}, nil
}

func (r *RemoteChaosClient) Delete(ctx context.Context, ref Ref) error {
	dyn, err := r.dyn()
	if err != nil {
		return err
	}
	gvr := schema.GroupVersionResource{Group: ref.Group, Version: ref.Version, Resource: ref.Resource}
	err = dyn.Resource(gvr).Namespace(ref.Namespace).Delete(ctx, ref.Name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *RemoteChaosClient) DeleteByRun(ctx context.Context, runID string) error {
	refs, err := r.ListByRun(ctx, runID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, ref := range refs {
		if err := r.Delete(ctx, ref); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *RemoteChaosClient) ListByRun(ctx context.Context, runID string) ([]Ref, error) {
	dyn, err := r.dyn()
	if err != nil {
		return nil, err
	}
	var out []Ref
	for _, gvr := range chaosGVRs() {
		list, err := dyn.Resource(gvr).Namespace(r.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "dlh.run-id=" + runID,
		})
		if err != nil {
			continue
		}
		for _, item := range list.Items {
			out = append(out, Ref{
				Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource,
				Namespace: r.Namespace, Name: item.GetName(),
			})
		}
	}
	return out, nil
}

func (r *RemoteChaosClient) ListManaged(ctx context.Context) (map[string][]Ref, error) {
	dyn, err := r.dyn()
	if err != nil {
		return nil, err
	}
	out := map[string][]Ref{}
	for _, gvr := range chaosGVRs() {
		list, err := dyn.Resource(gvr).Namespace(r.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "dlh.managed-by=controlplane",
		})
		if err != nil {
			continue
		}
		for _, item := range list.Items {
			runID := item.GetLabels()["dlh.run-id"]
			if runID == "" {
				continue
			}
			out[runID] = append(out[runID], Ref{
				Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource,
				Namespace: r.Namespace, Name: item.GetName(),
			})
		}
	}
	return out, nil
}
```

- [ ] **Step 2: Compile-time interface conformance check.** Append:

```go
var _ Client = (*RemoteChaosClient)(nil)
```

- [ ] **Step 3: Tests are limited** because we can't easily fake a real `*rest.Config`. Add a guard test:

```go
package chaos

import (
	"context"
	"testing"
)

func TestRemoteChaosClient_NoRestConfig(t *testing.T) {
	r := &RemoteChaosClient{Namespace: "dlh-test-fw", TargetID: "x"}
	_, err := r.Create(context.Background(), "run-1", Resource{
		APIVersion: "chaos-mesh.org/v1alpha1", Kind: "Schedule",
		Metadata: map[string]interface{}{"name": "x"},
		Spec:     map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error when RestConfig is nil")
	}
}
```

(Real integration with a remote cluster is in Task 23's smoke.)

- [ ] **Step 4: Build + test.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17/controlplane
go build ./...
go test ./internal/chaos/... -v
```

Expected: existing 6 chaos tests + 1 new = 7 pass.

- [ ] **Step 5: Commit.**

```bash
git add controlplane/internal/chaos/remote.go controlplane/internal/chaos/remote_test.go
git commit -m "feat(controlplane/chaos): RemoteChaosClient impl

Same Client interface as LocalChaosClient; builds a dynamic client per
call from the target's *rest.Config. Adds a dlh.target label so the
watchdog can list cross-cluster chaos in a single pass."
```

---

## Task 11: Router — pick Local vs Remote per call

**Files:**
- Create: `controlplane/internal/chaos/router.go`
- Create: `controlplane/internal/chaos/router_test.go`

- [ ] **Step 1: Write `internal/chaos/router.go`.**

```go
package chaos

import (
	"context"
	"fmt"

	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
)

// Router picks Local or Remote chaos client per call based on targetID.
// Implements the Client interface so existing callers (handlers + watchdog)
// can use it transparently.
//
// The Client interface methods don't carry a targetID; we add targetID-aware
// methods on Router for handler use. Callers that need targeting use
// CreateForTarget; the existing methods route to Local (empty target).
type Router struct {
	Local    Client
	Registry *targets.Registry
}

// CreateForTarget routes by targetID. Empty targetID = local.
func (r *Router) CreateForTarget(ctx context.Context, runID, targetID string, res Resource) (Ref, error) {
	c, err := r.pick(targetID)
	if err != nil {
		return Ref{}, err
	}
	return c.Create(ctx, runID, res)
}

// DeleteForTarget routes by targetID. Empty = local.
func (r *Router) DeleteForTarget(ctx context.Context, targetID string, ref Ref) error {
	c, err := r.pick(targetID)
	if err != nil {
		return err
	}
	return c.Delete(ctx, ref)
}

// Existing Client-interface methods route to Local. The watchdog will use
// ListManaged across all targets via a new helper below.
func (r *Router) Create(ctx context.Context, runID string, res Resource) (Ref, error) {
	return r.Local.Create(ctx, runID, res)
}
func (r *Router) Delete(ctx context.Context, ref Ref) error {
	return r.Local.Delete(ctx, ref)
}
func (r *Router) DeleteByRun(ctx context.Context, runID string) error {
	// Best-effort cross-cluster cleanup: walk every known client.
	var firstErr error
	clients := r.allClients()
	for _, c := range clients {
		if err := c.DeleteByRun(ctx, runID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
func (r *Router) ListByRun(ctx context.Context, runID string) ([]Ref, error) {
	var out []Ref
	for _, c := range r.allClients() {
		refs, err := c.ListByRun(ctx, runID)
		if err != nil {
			continue
		}
		out = append(out, refs...)
	}
	return out, nil
}
func (r *Router) ListManaged(ctx context.Context) (map[string][]Ref, error) {
	out := map[string][]Ref{}
	for _, c := range r.allClients() {
		got, err := c.ListManaged(ctx)
		if err != nil {
			continue
		}
		for k, v := range got {
			out[k] = append(out[k], v...)
		}
	}
	return out, nil
}

func (r *Router) pick(targetID string) (Client, error) {
	if targetID == "" {
		return r.Local, nil
	}
	if r.Registry == nil {
		return nil, fmt.Errorf("router: no registry for target %q", targetID)
	}
	t := r.Registry.Get(targetID)
	if t == nil {
		return nil, fmt.Errorf("router: unknown target %q", targetID)
	}
	return &RemoteChaosClient{
		RestConfig: t.RestConfig,
		Namespace:  t.Namespace,
		TargetID:   t.ID,
	}, nil
}

func (r *Router) allClients() []Client {
	clients := []Client{r.Local}
	if r.Registry == nil {
		return clients
	}
	for _, t := range r.Registry.List() {
		if t.RestConfig == nil {
			continue
		}
		clients = append(clients, &RemoteChaosClient{
			RestConfig: t.RestConfig,
			Namespace:  t.Namespace,
			TargetID:   t.ID,
		})
	}
	return clients
}

// Compile-time check
var _ Client = (*Router)(nil)
```

- [ ] **Step 2: Write `internal/chaos/router_test.go`.**

```go
package chaos

import (
	"context"
	"testing"

	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
)

func TestRouter_PickLocalOnEmptyTargetID(t *testing.T) {
	local := newDynFake() // from local_test.go
	r := &Router{Local: local}
	c, err := r.pick("")
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if c != local {
		t.Errorf("expected local client, got %T", c)
	}
}

func TestRouter_PickUnknownTarget(t *testing.T) {
	r := &Router{Local: newDynFake(), Registry: targets.NewRegistry()}
	_, err := r.pick("nope")
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
}

func TestRouter_CreateForTarget_EmptyRoutesLocal(t *testing.T) {
	local := newDynFake()
	r := &Router{Local: local}
	ref, err := r.CreateForTarget(context.Background(), "run-1", "", Resource{
		APIVersion: "chaos-mesh.org/v1alpha1",
		Kind:       "Schedule",
		Metadata:   map[string]interface{}{"name": "sched-x"},
		Spec:       map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ref.Name != "sched-x" {
		t.Errorf("ref name: %q", ref.Name)
	}
	// Confirm it landed in local
	refs, _ := local.ListByRun(context.Background(), "run-1")
	if len(refs) != 1 {
		t.Errorf("expected 1 local ref, got %d", len(refs))
	}
}
```

- [ ] **Step 3: Build + test.**

```bash
go build ./...
go test ./internal/chaos/... -v
```

Expected: 10 chaos tests pass (7 existing + 3 router).

- [ ] **Step 4: Commit.**

```bash
git add controlplane/internal/chaos/router.go controlplane/internal/chaos/router_test.go
git commit -m "feat(controlplane/chaos): Router picks Local vs Remote per call

Empty targetID routes to Local (preserves Phase C behaviour); non-empty
resolves via the targets registry. ListManaged + DeleteByRun fan out
across all configured clients so the watchdog reaps cross-cluster chaos
in one pass."
```

---

## Task 12: /internal/chaos reads targetID query param

**Files:**
- Modify: `controlplane/internal/api/internal_chaos.go`
- Modify: `controlplane/internal/api/server.go` (Deps swaps chaos.Client for *chaos.Router)
- Modify: `controlplane/cmd/dlh-controlplane/main.go` (build Router from registry)

- [ ] **Step 1: In `internal/api/server.go`**, change `Chaos chaos.Client` to `Chaos *chaos.Router`. Update the import to import the chaos package (it should already be there).

- [ ] **Step 2: Update `internal/api/internal_chaos.go`** to read `targetID` and use Router.CreateForTarget / DeleteForTarget:

Replace the existing Create / Delete bodies:

```go
func (h *InternalChaosHandler) Create(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("runID")
	if runID == "" {
		http.Error(w, "runID query param required", http.StatusBadRequest)
		return
	}
	targetID := r.URL.Query().Get("targetID") // optional; "" = local
	var res chaos.Resource
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	ref, err := h.Chaos.CreateForTarget(r.Context(), runID, targetID, res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"ref":       ref.Encode(),
		"kind":      ref.Resource,
		"name":      ref.Name,
		"namespace": ref.Namespace,
		"targetID":  targetID,
	})
}

func (h *InternalChaosHandler) Delete(w http.ResponseWriter, r *http.Request) {
	refStr := chi.URLParam(r, "ref")
	if refStr == "" {
		http.Error(w, "ref required", http.StatusBadRequest)
		return
	}
	targetID := r.URL.Query().Get("targetID")
	ref, err := chaos.DecodeRef(refStr)
	if err != nil {
		http.Error(w, "invalid ref: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.Chaos.DeleteForTarget(r.Context(), targetID, ref); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

Also update the `InternalChaosHandler` struct definition (`Chaos *chaos.Router` instead of `Chaos chaos.Client`).

- [ ] **Step 3: In `cmd/dlh-controlplane/main.go`**, replace the existing chaos client wiring:

Find:
```go
chaosClient := &chaos.LocalChaosClient{Dyn: clients.Dynamic, Namespace: cfg.K8sNamespace}
```

Replace with:
```go
localChaos := &chaos.LocalChaosClient{Dyn: clients.Dynamic, Namespace: cfg.K8sNamespace}
chaosRouter := &chaos.Router{Local: localChaos, Registry: targetsReg}
```

Update the `Deps` literal to use `Chaos: chaosRouter` (was `Chaos: chaosClient`).

Update the watchdog construction to use the router (it already satisfies Client):

```go
watchdog := &chaos.Watchdog{Chaos: chaosRouter, RunsTerminal: checker, Interval: 30 * time.Second}
```

- [ ] **Step 4: Update any test files that constructed a `Deps{Chaos: ...}` with a non-Router**. Search:

```bash
grep -rn "Chaos:" controlplane/ | grep -v gen.go
```

Update any test fixtures: most likely just `internal/api/handlers_test.go` doesn't set Chaos directly (Phase C constructed it only via main). If any test does, swap to `&chaos.Router{Local: <fake>}`.

- [ ] **Step 5: Build + test.**

```bash
go build ./...
go test ./...
```

Expected: clean build; all tests pass.

- [ ] **Step 6: Commit.**

```bash
git add controlplane/internal/api/server.go controlplane/internal/api/internal_chaos.go \
        controlplane/cmd/dlh-controlplane/main.go
git commit -m "feat(controlplane): /internal/chaos honours targetID query param

POST/DELETE /internal/chaos now read targetID from the URL query and
route through chaos.Router. Empty targetID = local (Phase C behaviour
preserved). Watchdog uses the Router so it reaps cross-cluster chaos
in one pass."
```

---

## Task 13: Update CancelRun to delete chaos across all targets

**Files:**
- Modify: `controlplane/internal/api/handlers.go` (CancelRun now uses Router.DeleteByRun which fans out)

- [ ] **Step 1: Verify CancelRun's existing impl already calls DeleteByRun.**

```bash
grep -A 8 "func.*CancelRun" controlplane/internal/api/handlers.go
```

It should already call `h.deps.Chaos.DeleteByRun`. The Router's DeleteByRun fans out — no code change needed if the call exists.

- [ ] **Step 2: If the existing code passes** (Phase C wired `Chaos chaos.Client` and `Chaos.DeleteByRun` works on the Router too), this task is just verification:

```bash
go build ./...
go test ./internal/api/... -v
```

Expected: all api tests pass.

- [ ] **Step 3: Commit only if changes were needed.** Otherwise skip.

```bash
# Only if a code change happened:
git add controlplane/internal/api/handlers.go
git commit -m "feat(controlplane/api): CancelRun cleans chaos across all targets via Router fan-out"
```

If no change needed, write a no-op commit with just a documentation note? **No** — skip the commit. Task 13's purpose is to confirm the existing path works.

---

## Task 14: Watchdog cross-cluster reconciliation

**Files:**
- Modify: `controlplane/internal/chaos/watchdog.go` (optionally — Router already handles fan-out via ListManaged)
- Modify: `controlplane/internal/chaos/watchdog_test.go` (extend test)

- [ ] **Step 1: Verify the existing watchdog's ListManaged call resolves to Router's fan-out.**

```bash
grep "ListManaged" controlplane/internal/chaos/watchdog.go
```

Should call `w.Chaos.ListManaged(ctx)`. Since `Chaos` is the `Client` interface and Router satisfies it, fan-out happens automatically. No code change needed.

- [ ] **Step 2: Add a cross-cluster test** to lock the contract.

Append to `internal/chaos/watchdog_test.go`:

```go
func TestWatchdog_FanOutAcrossTargets(t *testing.T) {
	// Build a fake "cross-cluster" by stacking LocalChaosClients in a Router.
	// Local has run-running's chaos. A second LocalChaosClient acts as a
	// pretend remote and has run-finished's chaos. Watchdog should reap
	// only run-finished.
	local := newDynFake()
	_, _ = local.Create(context.Background(), "run-running", Resource{
		APIVersion: "chaos-mesh.org/v1alpha1", Kind: "Schedule",
		Metadata: map[string]interface{}{"name": "sched-local"},
		Spec:     map[string]interface{}{},
	})
	remote := newDynFake()
	_, _ = remote.Create(context.Background(), "run-finished", Resource{
		APIVersion: "chaos-mesh.org/v1alpha1", Kind: "Schedule",
		Metadata: map[string]interface{}{"name": "sched-remote"},
		Spec:     map[string]interface{}{},
	})

	// We can't trivially add a fake remote to the Router (Router builds
	// RemoteChaosClient from kubeconfig). Instead test the watchdog
	// against a Router-like fake that returns merged ListManaged.
	merged := &fakeChaosForWatchdog{
		managed: map[string][]Ref{
			"run-running":  {{Name: "sched-local"}},
			"run-finished": {{Name: "sched-remote"}},
		},
	}
	w := &Watchdog{
		Chaos: merged,
		RunsTerminal: RunsTerminalCheckerFunc(func(runID string) bool {
			return runID == "run-finished"
		}),
		Interval: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	deleted := merged.deletedSnapshot()
	if len(deleted) == 0 {
		t.Fatal("expected deletions")
	}
	for _, d := range deleted {
		if d.Name == "sched-local" {
			t.Errorf("running run's chaos should not be reaped: %+v", d)
		}
	}
}
```

(This test exercises the Watchdog logic with a fake Client. Router-level fan-out is exercised in router_test.go's existing tests + Task 23's e2e smoke.)

- [ ] **Step 2: Build + test.**

```bash
go build ./...
go test ./internal/chaos/... -v
```

Expected: 11 chaos tests pass (10 existing + 1 new).

- [ ] **Step 3: Commit.**

```bash
git add controlplane/internal/chaos/watchdog_test.go
git commit -m "test(controlplane/chaos): watchdog fan-out across targets

Verifies the watchdog reaps a terminal run's chaos regardless of which
cluster it lives in (Router.ListManaged unions across all registered
clients)."
```

**Section B complete.** Chaos can be created/deleted on remote clusters. Router routes by targetID. Watchdog reaps cross-cluster.

---

# Section C — Run.Target end-to-end + manifest by-target index (Tasks 15-18)

## Task 15: CreateRun threads targetId

**Files:**
- Modify: `controlplane/internal/runs/submit.go` (SubmitRequest.TargetID, Workflow gets dlh.target label)
- Modify: `controlplane/internal/api/handlers.go` (CreateRun reads body.TargetId)
- Modify: `controlplane/internal/runs/submit_test.go`

- [ ] **Step 1: Extend SubmitRequest.**

In `internal/runs/submit.go`, find `SubmitRequest`:

```go
type SubmitRequest struct {
	ScenarioID string
	TargetID   string
	Parameters map[string]string
	CreatedBy  string
}
```

In the same file's Submit function, when constructing the Workflow CR:

- Add `"dlh.target": req.TargetID` to the Labels map (only if `TargetID != ""`).
- Add a workflow argument so chaos WT http steps can pick it up: in `wf.Spec.Arguments.Parameters`, append an entry with name `target_id` and value `req.TargetID`.

Code:

```go
labels := map[string]string{
	"dlh.scenario": req.ScenarioID,
	"dlh.run-id":   runID,
}
if req.TargetID != "" {
	labels["dlh.target"] = req.TargetID
}

params := make([]wfv1.Parameter, 0, len(req.Parameters)+1)
for k, v := range req.Parameters {
	val := wfv1.AnyString(v)
	params = append(params, wfv1.Parameter{Name: k, Value: &val})
}
// Always add target_id (empty for local); chaos WT http steps reference it.
tidVal := wfv1.AnyString(req.TargetID)
params = append(params, wfv1.Parameter{Name: "target_id", Value: &tidVal})
```

(Replace the existing labels/params block accordingly.)

Also extend `SubmitResult`:

```go
type SubmitResult struct {
	RunID     string
	TargetID  string
	StartedAt time.Time
}
```

And set `TargetID: req.TargetID` in the return value.

- [ ] **Step 2: Update `internal/api/handlers.go` `CreateRun`** to read `body.TargetId` and pass it through:

```go
targetID := ""
if body.TargetId != nil {
	targetID = *body.TargetId
}
sr, err := h.deps.Submitter.Submit(ctx, runs.SubmitRequest{
	ScenarioID: body.ScenarioId,
	TargetID:   targetID,
	Parameters: params,
	CreatedBy:  createdBy,
})
```

And in the response Run, populate Target if non-empty:

```go
resp := gen.Run{
	Id:           sr.RunID,
	Scenario:     body.ScenarioId,
	Status:       gen.RunStatus("Submitted"),
	StartedAt:    sr.StartedAt,
	WorkflowName: stringPtr(sr.RunID),
}
if targetID != "" {
	resp.Target = &targetID
}
return gen.CreateRun202JSONResponse(resp), nil
```

(Confirm `gen.Run.Target` field exists — should after Task 8 codegen.)

- [ ] **Step 3: Update the initial Submitted manifest** in CreateRun:

```go
m := runs.Manifest{
	RunID:        sr.RunID,
	Scenario:     body.ScenarioId,
	Target:       targetID,
	WorkflowName: sr.RunID,
	Parameters:   params,
	CreatedBy:    createdBy,
	Status:       "Submitted",
	StartedAt:    sr.StartedAt,
}
```

`Manifest.Target` doesn't exist yet — Task 17 adds it. For now, this won't compile. Workaround: comment out the Target line in this commit, uncomment in Task 17. Or write Task 17 first. Plan: do Task 17 inline-and-after-this, then add the line.

Cleaner sequence: write the Manifest.Target field first.

**REVISE:** Move Task 17's Manifest.Target field addition into Task 15 Step 3 as a prerequisite. Modify Task 17 to focus solely on the by-target index write.

Add to `internal/runs/manifest.go` `Manifest` struct (insert in alphabetical order):

```go
Target string `json:"target,omitempty"`
```

Now Step 3's manifest construction compiles.

- [ ] **Step 4: Extend the submit test.**

In `internal/runs/submit_test.go`, append:

```go
func TestSubmit_WithTargetID(t *testing.T) {
	ns := "dlh-test-fw"
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	s := &Submitter{Argo: argo, Namespace: ns}
	res, err := s.Submit(context.Background(), SubmitRequest{
		ScenarioID: "mysql-pod-delete",
		TargetID:   "staging-mysql",
		CreatedBy:  "tester",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res.TargetID != "staging-mysql" {
		t.Errorf("TargetID echo: %q", res.TargetID)
	}
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res.RunID, metav1.GetOptions{})
	if got.Labels["dlh.target"] != "staging-mysql" {
		t.Errorf("dlh.target label: %v", got.Labels)
	}
	foundTargetArg := false
	for _, p := range got.Spec.Arguments.Parameters {
		if p.Name == "target_id" && p.Value != nil && p.Value.String() == "staging-mysql" {
			foundTargetArg = true
			break
		}
	}
	if !foundTargetArg {
		t.Errorf("target_id parameter not propagated: %+v", got.Spec.Arguments.Parameters)
	}
}
```

- [ ] **Step 5: Build + test.**

```bash
go build ./...
go test ./internal/runs/... ./internal/api/... -v
```

Expected: 4 submit tests + existing api tests pass.

- [ ] **Step 6: Commit.**

```bash
git add controlplane/internal/runs/submit.go controlplane/internal/runs/submit_test.go \
        controlplane/internal/runs/manifest.go \
        controlplane/internal/api/handlers.go
git commit -m "feat(controlplane/runs): CreateRun threads targetId end-to-end

SubmitRequest gains TargetID. Workflow CR gets dlh.target label +
target_id workflow argument so chaos WT http steps can forward it to
/internal/chaos. Manifest gains Target field. Response Run carries
target field for the UI."
```

---

## Task 16: Syncer propagates Target into manifest updates

**Files:**
- Modify: `controlplane/internal/runs/syncer.go`
- Modify: `controlplane/internal/runs/syncer_test.go`

- [ ] **Step 1: In `internal/runs/syncer.go` `handle()`**, read the `dlh.target` label and put it in the Manifest:

```go
m := Manifest{
	RunID:        runID,
	Scenario:     wf.Labels["dlh.scenario"],
	Target:       wf.Labels["dlh.target"],
	WorkflowName: wf.Name,
	Status:       status,
	StartedAt:    wf.CreationTimestamp.Time,
}
```

- [ ] **Step 2: Extend a syncer test** to verify the propagation.

Append to `internal/runs/syncer_test.go`:

```go
func TestSyncer_PropagatesTarget(t *testing.T) {
	src := &fakeEventSource{ch: make(chan k8s.WorkflowEvent, 2)}
	sink := &captureSink{}
	s := &Syncer{Source: src, Manifests: sink}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name: "run-1",
			Labels: map[string]string{
				"dlh.run-id":   "run-1",
				"dlh.scenario": "mysql-pod-delete",
				"dlh.target":   "staging-mysql",
			},
			CreationTimestamp: metav1.NewTime(time.Now()),
		},
		Status: wfv1.WorkflowStatus{Phase: "Running"},
	}
	src.ch <- k8s.WorkflowEvent{Type: "ADDED", Workflow: wf}
	time.Sleep(300 * time.Millisecond)
	got := sink.snapshot()
	if len(got) == 0 {
		t.Fatal("no manifest written")
	}
	if got[len(got)-1].Target != "staging-mysql" {
		t.Errorf("Target propagation: %q", got[len(got)-1].Target)
	}
}
```

- [ ] **Step 3: Build + test.**

```bash
go test ./internal/runs/... -v
```

Expected: 5 runs tests pass.

- [ ] **Step 4: Commit.**

```bash
git add controlplane/internal/runs/syncer.go controlplane/internal/runs/syncer_test.go
git commit -m "feat(controlplane/runs): Syncer propagates dlh.target label into manifests"
```

---

## Task 17: Manifest by-target index write path

**Files:**
- Modify: `controlplane/internal/runs/manifest.go`
- Modify: `controlplane/internal/runs/manifest_test.go`

- [ ] **Step 1: Extend ManifestWriter.Write to also put an index/by-target object** when Target is set:

In `Write`:

```go
day := m.StartedAt.UTC().Format("2006-01-02")
idx := fmt.Sprintf("runs/index/by-scenario/%s/%s/%s.json", sanitize(m.Scenario), day, m.RunID)
if err := w.putJSON(ctx, idx, body); err != nil {
	return fmt.Errorf("put index: %w", err)
}
if m.Target != "" {
	idxT := fmt.Sprintf("runs/index/by-target/%s/%s/%s.json", sanitize(m.Target), day, m.RunID)
	if err := w.putJSON(ctx, idxT, body); err != nil {
		return fmt.Errorf("put by-target index: %w", err)
	}
}
```

- [ ] **Step 2: Add a test.**

Append to `internal/runs/manifest_test.go`:

```go
func TestManifest_HasTargetField(t *testing.T) {
	m := Manifest{
		RunID:    "x",
		Scenario: "mysql-pod-delete",
		Target:   "staging-mysql",
		Status:   "Running",
	}
	raw, _ := json.Marshal(m)
	if !strings.Contains(string(raw), `"target":"staging-mysql"`) {
		t.Errorf("target field missing in JSON: %s", raw)
	}
}
```

Add `"strings"` import to the test file.

- [ ] **Step 3: Build + test.**

```bash
go build ./...
go test ./internal/runs/...
```

Expected: 6 runs tests pass.

- [ ] **Step 4: Commit.**

```bash
git add controlplane/internal/runs/manifest.go controlplane/internal/runs/manifest_test.go
git commit -m "feat(controlplane/runs): ManifestWriter writes runs/index/by-target/{target}/...

Mirrors the by-scenario index. Only written when Manifest.Target is
non-empty (local runs unchanged)."
```

---

## Task 18: ListRuns supports target filter; GetRun returns target

**Files:**
- Modify: `controlplane/api/openapi.yaml` (add target query param to listRuns)
- Regenerate: `internal/api/gen/*.gen.go`, `web/src/api/gen.ts`
- Modify: `controlplane/internal/k8s/workflows.go` (`WorkflowFilter.Target`)
- Modify: `controlplane/internal/api/handlers.go` (read body filter, populate Run.Target on list)

- [ ] **Step 1: Add `target` query param** to listRuns in openapi.yaml.

Find the `/api/runs:` GET block. Add a parameter:

```yaml
        - in: query
          name: target
          schema: { type: string }
```

Add it after the existing `scenario` query param.

- [ ] **Step 2: Regenerate.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17/controlplane
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-server.yaml api/openapi.yaml
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-types.yaml api/openapi.yaml
cd web && pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts && cd ..
```

- [ ] **Step 3: Extend WorkflowFilter in `internal/k8s/workflows.go`** with a `Target string` field. Update `filterWorkflows` to also filter by `wf.Labels["dlh.target"]` when `f.Target != ""`.

```go
type WorkflowFilter struct {
	Scenario string
	Target   string
	Status   string
	Since    *time.Time
	Limit    int
}

func filterWorkflows(items []*wfv1.Workflow, f WorkflowFilter) []*wfv1.Workflow {
	out := []*wfv1.Workflow{}
	for _, w := range items {
		if f.Scenario != "" && w.Labels["dlh.scenario"] != f.Scenario {
			continue
		}
		if f.Target != "" && w.Labels["dlh.target"] != f.Target {
			continue
		}
		if f.Status != "" && string(w.Status.Phase) != f.Status {
			continue
		}
		if f.Since != nil && w.CreationTimestamp.Before(&metav1.Time{Time: *f.Since}) {
			continue
		}
		out = append(out, w)
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out
}
```

(Adjust to the existing exact filter implementation — preserve the existing time comparison.)

- [ ] **Step 4: Update `ListRuns` handler** to pass the new filter through:

```go
f := k8s.WorkflowFilter{}
if req.Params.Scenario != nil {
	f.Scenario = *req.Params.Scenario
}
if req.Params.Target != nil {
	f.Target = *req.Params.Target
}
...
```

And in `RunFromWorkflow` (in `internal/model/types.go`), populate the Target field:

```go
if v := wf.Labels["dlh.target"]; v != "" {
	r.Target = &v
}
```

(Verify `gen.Run.Target` is `*string`.)

- [ ] **Step 5: Build + test.**

```bash
go build ./...
go test ./...
```

Expected: clean build; all tests pass. If `WorkflowFilter` already has its own tests, add a `TestFilter_ByTarget` test in `internal/k8s/workflows_test.go`:

```go
func TestFilter_ByTarget(t *testing.T) {
	now := metav1.Now()
	items := []*wfv1.Workflow{
		{ObjectMeta: metav1.ObjectMeta{Name: "a", Labels: map[string]string{"dlh.target": "staging-mysql"}, CreationTimestamp: now}},
		{ObjectMeta: metav1.ObjectMeta{Name: "b", Labels: map[string]string{}, CreationTimestamp: now}},
	}
	got := filterWorkflows(items, WorkflowFilter{Target: "staging-mysql"})
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("got %+v", got)
	}
}
```

- [ ] **Step 6: Commit.**

```bash
git add controlplane/api/openapi.yaml controlplane/internal/api/gen/ controlplane/web/src/api/gen.ts \
        controlplane/internal/k8s/workflows.go controlplane/internal/k8s/workflows_test.go \
        controlplane/internal/api/handlers.go controlplane/internal/model/types.go
git commit -m "feat(controlplane): ListRuns ?target= filter; Run.Target populated from label

WorkflowFilter gains Target. ListRuns reads ?target=&. Phase B's
RunFromWorkflow now propagates dlh.target into the Run DTO so the UI
+ CLI can show + filter by target."
```

**Section C complete.** Run.Target is wired end-to-end from API → Workflow → manifest → API.

---

# Section D — UI + CLI + WT rewiring + chaos-mesh-per-target docs (Tasks 19-22)

## Task 19: WorkflowTemplate http steps forward target_id

**Files:**
- Modify: `helm/dlh-test-fw/files/workflowtemplates/chaos/pod-delete.yaml`
- Modify: `helm/dlh-test-fw/files/workflowtemplates/chaos/network-loss.yaml`
- Modify: `helm/dlh-test-fw/files/workflowtemplates/chaos/kafka-broker-partition.yaml`

The chaos WTs need to (a) accept `target_id` as an input parameter (default `""`), (b) pass `targetID` query param to the POST + DELETE http steps.

- [ ] **Step 1: For each of the 3 WTs**, do the following replacements via Edit:

  **A. Add `target_id` to `main.inputs.parameters`** with a default of empty:

  ```yaml
        - { name: target_id, value: "" }
  ```

  (Insert at the end of the parameters list. Note the explicit `value: ""` since Argo wants a default.)

  **B. Pass `target_id` into the `post-chaos` template arguments:**

  Find the `post` step that calls `post-chaos`. Update its arguments to include:

  ```yaml
          - { name: target_id, value: '{{`{{inputs.parameters.target_id}}`}}' }
  ```

  **C. Update `post-chaos`** to accept `target_id` as an input parameter and put it in the URL query:

  ```yaml
    - name: post-chaos
      inputs:
        parameters:
        - name: body
        - name: target_id
      outputs:
        parameters:
        - name: ref
          valueFrom: { jsonPath: '$.ref' }
      http:
        url: 'http://dlh-controlplane.dlh-test-fw.svc.cluster.local:80/internal/chaos?runID={{`{{workflow.name}}`}}&targetID={{`{{inputs.parameters.target_id}}`}}'
        method: POST
        headers:
        - name: X-Internal-Token
          valueFrom: { secretKeyRef: { name: dlh-internal-token, key: token } }
        - name: Content-Type
          value: application/json
        body: '{{`{{inputs.parameters.body}}`}}'
  ```

  **D. Similarly update `cleanup-chaos`** to accept + forward `target_id`:

  ```yaml
    - name: cleanup-chaos
      inputs:
        parameters:
        - name: ref
        - name: target_id
      http:
        url: 'http://dlh-controlplane.dlh-test-fw.svc.cluster.local:80/internal/chaos/{{`{{inputs.parameters.ref}}`}}?targetID={{`{{inputs.parameters.target_id}}`}}'
        method: DELETE
        headers:
        - name: X-Internal-Token
          valueFrom: { secretKeyRef: { name: dlh-internal-token, key: token } }
  ```

  **E. Pass `target_id` to the cleanup step from `main`:**

  ```yaml
      - - name: cleanup
          template: cleanup-chaos
          arguments:
            parameters:
            - { name: ref, value: '{{`{{steps.post.outputs.parameters.ref}}`}}' }
            - { name: target_id, value: '{{`{{inputs.parameters.target_id}}`}}' }
  ```

Repeat for all 3 chaos WTs.

- [ ] **Step 2: Render + validate.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17
helm dependency update helm/dlh-test-fw 2>&1 | tail -3
helm template dlh helm/dlh-test-fw > /tmp/rendered-plan17.yaml
grep -c 'targetID=' /tmp/rendered-plan17.yaml
```

Expected: at least 6 matches (3 WTs × 2 calls each).

- [ ] **Step 3: Commit.**

```bash
git add helm/dlh-test-fw/files/workflowtemplates/chaos/
git commit -m "feat(workflowtemplates/chaos): forward target_id through /internal/chaos calls

Each chaos WT now accepts target_id (default empty) and threads it into
the URL query string of both POST and DELETE /internal/chaos. Empty
preserves Phase C local behaviour; non-empty triggers Router.CreateForTarget
to use RemoteChaosClient."
```

---

## Task 20: UI — Targets page + TargetPicker + Run target display

**Files:**
- Create: `controlplane/web/src/pages/TargetsPage.tsx`
- Create: `controlplane/web/src/components/TargetPicker.tsx`
- Modify: `controlplane/web/src/App.tsx`
- Modify: `controlplane/web/src/pages/ScenariosPage.tsx`
- Modify: `controlplane/web/src/pages/RunsPage.tsx`
- Modify: `controlplane/web/src/pages/RunDetailPage.tsx`

- [ ] **Step 1: Write `controlplane/web/src/pages/TargetsPage.tsx`.**

```tsx
import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/gen";

type Target = components["schemas"]["Target"];

export function TargetsPage() {
  const [items, setItems] = useState<Target[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [testing, setTesting] = useState<string | null>(null);
  const [results, setResults] = useState<Record<string, string>>({});

  const reload = () =>
    api.GET("/api/targets", {}).then(({ data, error }) => {
      if (error) setError(JSON.stringify(error));
      else setItems(data?.items ?? []);
    });

  useEffect(() => {
    reload();
  }, []);

  const testConn = async (id: string) => {
    setTesting(id);
    try {
      const { data, error } = await api.POST("/api/targets/{id}/test", {
        params: { path: { id } },
      });
      if (error) {
        setResults((r) => ({ ...r, [id]: `error: ${JSON.stringify(error)}` }));
      } else {
        setResults((r) => ({
          ...r,
          [id]: data?.ok
            ? `OK (${Math.round((data.latencyNanos ?? 0) / 1_000_000)} ms)`
            : `FAIL: ${data?.error ?? "unknown"}`,
        }));
      }
    } finally {
      setTesting(null);
    }
  };

  if (error) return <p className="text-rose-700">Error: {error}</p>;
  if (!items) return <p>Loading…</p>;
  return (
    <section>
      <h1 className="mb-4 text-xl font-semibold">Targets</h1>
      {items.length === 0 ? (
        <p className="text-slate-600">
          No targets registered. Targets are added by PR — see{" "}
          <code>docs/operations/register-target.md</code>.
        </p>
      ) : (
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-slate-200 text-left text-slate-600">
              <th className="py-2">ID</th>
              <th>Display Name</th>
              <th>Namespace</th>
              <th>Allowed Types</th>
              <th>Configured</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((t) => (
              <tr key={t.id} className="border-b border-slate-100">
                <td className="py-2">{t.id}</td>
                <td>{t.displayName ?? t.id}</td>
                <td>{t.namespace ?? "—"}</td>
                <td>{(t.allowedTargetTypes ?? []).join(", ") || "—"}</td>
                <td>
                  {t.configured ? (
                    <span className="text-emerald-700">✓</span>
                  ) : (
                    <span className="text-rose-700">✗</span>
                  )}
                </td>
                <td>
                  <button
                    onClick={() => testConn(t.id)}
                    disabled={testing === t.id}
                    className="rounded border border-slate-300 px-2 py-0.5 text-xs hover:bg-slate-100"
                  >
                    {testing === t.id ? "testing…" : "test"}
                  </button>
                  {results[t.id] && (
                    <span className="ml-2 text-xs text-slate-600">{results[t.id]}</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
```

- [ ] **Step 2: Write `controlplane/web/src/components/TargetPicker.tsx`.**

```tsx
import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/gen";

type Target = components["schemas"]["Target"];

export function TargetPicker({
  value,
  onChange,
  filterType,
}: {
  value: string;
  onChange: (id: string) => void;
  filterType?: string;
}) {
  const [items, setItems] = useState<Target[] | null>(null);

  useEffect(() => {
    api.GET("/api/targets", {}).then(({ data }) => {
      setItems(data?.items ?? []);
    });
  }, []);

  if (!items) return <span className="text-xs text-slate-500">loading targets…</span>;
  const filtered = filterType
    ? items.filter(
        (t) =>
          !t.allowedTargetTypes?.length || t.allowedTargetTypes.includes(filterType)
      )
    : items;

  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="rounded border border-slate-300 bg-white px-2 py-1 text-sm"
    >
      <option value="">(local — framework cluster)</option>
      {filtered
        .filter((t) => t.configured)
        .map((t) => (
          <option key={t.id} value={t.id}>
            {t.displayName ?? t.id}
          </option>
        ))}
    </select>
  );
}
```

- [ ] **Step 3: Update App.tsx** to add /targets route + nav link.

In `controlplane/web/src/App.tsx`, find the existing nav + Routes. Add:

```tsx
import { TargetsPage } from "./pages/TargetsPage";
```

In the nav: add a `<Link to="/targets">Targets</Link>` next to the others.

In the Routes: add `<Route path="/targets" element={<TargetsPage />} />`.

- [ ] **Step 4: Update ScenariosPage.tsx** to include the TargetPicker in the submit form (the current page may not have a submit form — it might just display the catalog. If so, add a minimal "Run" button per scenario that opens a small inline form with the picker. Concretely: each card gets a Run button that pops a form below it).

Read the existing ScenariosPage. If it currently displays a list without inline submit, add a "Run" button per scenario:

```tsx
import { TargetPicker } from "../components/TargetPicker";

// inside the component:
const [submitTarget, setSubmitTarget] = useState<Record<string, string>>({});
const [submitParams, setSubmitParams] = useState<Record<string, string>>({});
const [submitting, setSubmitting] = useState<string | null>(null);

const handleRun = async (s: Scenario) => {
  setSubmitting(s.id);
  try {
    const body: any = { scenarioId: s.id };
    if (submitTarget[s.id]) body.targetId = submitTarget[s.id];
    const { data, error } = await api.POST("/api/runs", { body });
    if (error) {
      alert("Failed: " + JSON.stringify(error));
    } else if (data?.id) {
      window.location.href = `/runs/${data.id}`;
    }
  } finally {
    setSubmitting(null);
  }
};

// inside each card render:
<div className="mt-3 flex items-center gap-2">
  <TargetPicker
    value={submitTarget[s.id] ?? ""}
    onChange={(v) => setSubmitTarget((r) => ({ ...r, [s.id]: v }))}
    filterType={s.targetType}
  />
  <button
    onClick={() => handleRun(s)}
    disabled={submitting === s.id}
    className="rounded bg-emerald-600 px-3 py-1 text-xs font-medium text-white hover:bg-emerald-700"
  >
    {submitting === s.id ? "submitting…" : "Run"}
  </button>
</div>
```

(Keep the rest of the card unchanged.)

- [ ] **Step 5: Update RunsPage.tsx + RunDetailPage.tsx** to show the Target column / field.

In `RunsPage.tsx`, add a "Target" column between Scenario and Status, displaying `r.target ?? "local"`.

In `RunDetailPage.tsx`, in the header section, show `run.target ? \`(target: ${run.target})\` : ""`.

- [ ] **Step 6: Build the UI.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17/controlplane/web
pnpm build 2>&1 | tail -10
```

Expected: clean build. If TypeScript complains about missing types, run `pnpm openapi-typescript ../api/openapi.yaml -o src/api/gen.ts` first.

- [ ] **Step 7: Commit.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17
git add controlplane/web/src/
git commit -m "feat(controlplane/web): Targets page + TargetPicker + per-card Run button

TargetsPage shows registered targets with a test-connection button.
TargetPicker dropdown is embedded in each scenario card on ScenariosPage
so users can choose a target before clicking Run. RunsPage + RunDetailPage
display the target column / field. Filter by scenario.targetType when
selecting eligible targets."
```

---

## Task 21: CLI — dlh run --target flag

**Files:**
- Modify: `controlplane/cmd/dlh/run.go`
- Modify: `controlplane/cmd/dlh/runs.go` (`runs ls --target` flag)

- [ ] **Step 1: Add a `--target` flag to `dlh run`.**

In `controlplane/cmd/dlh/run.go`, find the `runCmd` function. Add to the existing flags block:

```go
var target string
c.Flags().StringVar(&target, "target", "", "Optional remote target ID")
```

And in the RunE handler, include it in the body when non-empty:

```go
body := map[string]any{"scenarioId": scenario}
if target != "" {
	body["targetId"] = target
}
if len(params) > 0 {
	body["parameters"] = params
}
```

- [ ] **Step 2: Add a `--target` filter to `dlh runs ls`.**

In `controlplane/cmd/dlh/runs.go` `runsLsCmd`, add:

```go
var target string
c.Flags().StringVar(&target, "target", "", "Filter by target id")
```

In the RunE handler, propagate to the query:

```go
if target != "" {
	q.Set("target", target)
}
```

(Add a new column to the tabwriter output too — TARGET — after SCENARIO.)

- [ ] **Step 3: Build + smoke.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17/controlplane
make cli
./bin/dlh run --help | grep -i target
./bin/dlh runs ls --help | grep -i target
```

Expected: both show a `--target` flag.

- [ ] **Step 4: Commit.**

```bash
git add controlplane/cmd/dlh/run.go controlplane/cmd/dlh/runs.go
git commit -m "feat(controlplane/cli): dlh run --target + dlh runs ls --target

Both commands now honour the targetId field. dlh runs ls also gains
a TARGET column in the tabwriter output."
```

---

## Task 22: Operator docs + per-target chaos-mesh example

**Files:**
- Create: `docs/operations/register-target.md`
- Create: `argocd/apps/dlh-target-chaos-mesh.yaml.example`
- Create: `controlplane/deploy/targets-rbac.yaml.example`

- [ ] **Step 1: Write `docs/operations/register-target.md`.**

```markdown
# Registering a remote target cluster

Phase D allows the controlplane to inject chaos into clusters other than
the framework cluster. This document is the operator runbook.

## Prerequisites

1. The target cluster has chaos-mesh installed (see
   `argocd/apps/dlh-target-chaos-mesh.yaml.example` for a starter
   Argo CD Application that the operator commits + adapts).
2. A scoped ServiceAccount exists in the target cluster with:
   - chaos-mesh.org/* CRUD on schedules / podchaos / networkchaos
   - core/pods get + list (for selector resolution / probe)
   See `controlplane/deploy/targets-rbac.yaml.example`.
3. You have the kubeconfig of that ServiceAccount stored as a Secret
   (we recommend external-secrets / sealed-secrets / SOPS — Phase A's
   open question).

## Register

1. Create a Secret named `dlh-target-<id>` in the framework cluster's
   `dlh-test-fw` namespace, with key `kubeconfig` containing the bytes
   of the kubeconfig.
2. Add an entry to the `dlh-targets` ConfigMap in the same namespace:

   ```yaml
   data:
     targets.yaml: |
       targets:
         - id: staging-mysql
           displayName: "Staging MySQL"
           kubeconfigSecret: dlh-target-staging-mysql
           allowedTargetTypes: [mysql]
           namespace: dlh-test-fw  # chaos namespace on the target
   ```

3. Commit + push the change. Argo CD reconciles within ~30s; the
   controlplane refreshes its registry within another 30s.

## Verify

Browser UI → Targets page → click "test". Expected: green ✓ with a
millisecond latency reading.

CLI:
\`\`\`
dlh runs ls --target staging-mysql
\`\`\`

The first scenario run with `--target staging-mysql` should produce a
chaos CR in the **target** cluster's `dlh-test-fw` namespace (not the
framework cluster).
```

- [ ] **Step 2: Write `argocd/apps/dlh-target-chaos-mesh.yaml.example`.**

```yaml
# Operator template: copy + rename per target. Replace REPLACE-CLUSTER
# with the Argo CD destination cluster name (registered via
# `argocd cluster add`).
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: dlh-target-chaos-mesh-REPLACE-CLUSTER
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: dlh-test-fw
spec:
  project: dlh-test-fw
  source:
    chart: chaos-mesh
    repoURL: https://charts.chaos-mesh.org
    targetRevision: 2.8.2
    helm:
      values: |
        chaosDaemon:
          runtime: containerd
          socketPath: /run/containerd/containerd.sock
        dashboard:
          create: false
  destination:
    name: REPLACE-CLUSTER          # Argo CD-registered cluster name
    namespace: chaos-mesh
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true
```

- [ ] **Step 3: Write `controlplane/deploy/targets-rbac.yaml.example`.**

```yaml
# Apply this on each TARGET cluster (not the framework cluster) to give
# the controlplane a scoped ServiceAccount for chaos injection. The
# kubeconfig for this SA goes into the framework cluster's
# dlh-target-<id> Secret.
apiVersion: v1
kind: Namespace
metadata:
  name: dlh-test-fw
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: dlh-controlplane-remote
  namespace: dlh-test-fw
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: dlh-controlplane-remote
  namespace: dlh-test-fw
rules:
  - apiGroups: ["chaos-mesh.org"]
    resources: ["schedules", "podchaos", "networkchaos"]
    verbs: ["get", "list", "watch", "create", "delete"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: dlh-controlplane-remote
  namespace: dlh-test-fw
subjects:
  - kind: ServiceAccount
    name: dlh-controlplane-remote
    namespace: dlh-test-fw
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: dlh-controlplane-remote
```

- [ ] **Step 4: Commit.**

```bash
git add docs/operations/register-target.md argocd/apps/dlh-target-chaos-mesh.yaml.example \
        controlplane/deploy/targets-rbac.yaml.example
git commit -m "docs: Phase D — register a remote target cluster

Operator runbook + Argo CD Application template for installing
chaos-mesh on a target + scoped RBAC manifests for the target cluster's
ServiceAccount."
```

**Section D code complete.** Only smoke + docs + merge left.

---

# Section E — Smoke, docs, merge (Tasks 23-26)

## Task 23: Smoke against a second cluster (kind or second minikube profile)

**Files:** None modified normally; fix commits if bugs surface.

The goal: stand up a second cluster (kind is the easiest), install chaos-mesh into it via helm directly (skip the Argo CD path for the smoke), create a kubeconfig + register it as a target, run a scenario with `--target` and verify the chaos CR lands on the target cluster, not the framework cluster.

- [ ] **Step 1: Create a kind cluster named `dlh-target`.**

```bash
kind create cluster --name dlh-target --image kindest/node:v1.30.0 2>&1 | tail -5
kubectl --context kind-dlh-target get nodes
```

Expected: a 1-node Ready cluster.

- [ ] **Step 2: Install chaos-mesh on the kind cluster.**

```bash
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm --kube-context kind-dlh-target install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace chaos-mesh --create-namespace --version 2.8.2 \
  --set chaosDaemon.runtime=containerd \
  --set chaosDaemon.socketPath=/run/containerd/containerd.sock \
  --wait --timeout 4m 2>&1 | tail -5
```

Expected: chaos-mesh pods Running. (kind uses containerd by default — no docker-socket override needed.)

- [ ] **Step 3: Apply the target RBAC + create a kubeconfig.**

```bash
kubectl --context kind-dlh-target apply -f controlplane/deploy/targets-rbac.yaml.example

# Generate a kubeconfig that authenticates as dlh-controlplane-remote.
KIND_API=$(kubectl --context kind-dlh-target config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}')
KIND_CA=$(kubectl --context kind-dlh-target config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')

# Generate a token for the SA (k8s 1.24+ requires explicit Secret or `kubectl create token`).
TOKEN=$(kubectl --context kind-dlh-target -n dlh-test-fw create token dlh-controlplane-remote --duration=87600h)

cat > /tmp/dlh-target-kind.kubeconfig <<EOF
apiVersion: v1
kind: Config
clusters:
- name: kind
  cluster:
    server: $KIND_API
    certificate-authority-data: $KIND_CA
contexts:
- name: kind
  context:
    cluster: kind
    user: dlh-controlplane-remote
    namespace: dlh-test-fw
current-context: kind
users:
- name: dlh-controlplane-remote
  user:
    token: $TOKEN
EOF
```

Note: kind's API server runs on `127.0.0.1:<port>` which the minikube cluster CANNOT reach. Override `KIND_API` to use kind's docker network IP instead. This is a known kind quirk — Plan 17 documents it in FINDINGS.

```bash
# Discover kind's docker-network IP for the API server.
KIND_INTERNAL=$(docker inspect dlh-target-control-plane --format '{{ .NetworkSettings.Networks.kind.IPAddress }}')
KIND_API="https://$KIND_INTERNAL:6443"
# Rebuild the kubeconfig with the new server URL (re-run the heredoc).
sed -i.bak "s|server: .*|server: $KIND_API|" /tmp/dlh-target-kind.kubeconfig
```

Also: minikube's container network needs to be on the same docker network. Run:

```bash
docker network connect kind minikube 2>&1 || true   # already-connected is OK
docker network connect minikube dlh-target-control-plane 2>&1 || true
```

This is fiddly. If it fails, fall back to a second minikube profile instead — `minikube start -p dlh-target` shares the host docker network with the main minikube. Document whichever path worked.

- [ ] **Step 4: Register the target in the framework cluster.**

```bash
kubectl --context minikube -n dlh-test-fw create secret generic dlh-target-kind \
  --from-file=kubeconfig=/tmp/dlh-target-kind.kubeconfig

# Patch the dlh-targets ConfigMap to add this entry.
kubectl --context minikube -n dlh-test-fw patch configmap dlh-targets --type=merge -p='
{"data":{"targets.yaml":"targets:\n  - id: kind\n    displayName: \"Kind Target\"\n    kubeconfigSecret: dlh-target-kind\n    namespace: dlh-test-fw\n"}}'

# Wait for the controlplane registry refresh (≤30s).
sleep 35
```

- [ ] **Step 5: Verify the target is visible.**

```bash
# Port-forward, set DLH_ENDPOINT, fake token...
kubectl --context minikube -n dlh-test-fw port-forward svc/dlh-controlplane 18080:80 >/dev/null 2>&1 &
PF=$!
trap "kill $PF 2>/dev/null || true" EXIT
sleep 2
export DLH_ENDPOINT=http://localhost:18080
export DLH_TOKEN='fake:tester:tester@example.com:dlh-admins'

curl -fsS -H "Authorization: Bearer $DLH_TOKEN" $DLH_ENDPOINT/api/targets | jq .
```

Expected: items array with the `kind` target, `configured: true`.

- [ ] **Step 6: Test-connection.**

```bash
curl -fsS -X POST -H "Authorization: Bearer $DLH_TOKEN" $DLH_ENDPOINT/api/targets/kind/test | jq .
```

Expected: `{"ok": true, "latencyNanos": <small>}`.

- [ ] **Step 7: Submit a scenario with --target.**

Use the simplest scenario — `mysql-pod-delete` won't work (no mysql in the kind cluster). The cleanest test is the chaos-only side: a "smoke" scenario that just calls /internal/chaos directly. We don't have one. **For Phase D's smoke, accept that the e2e flow halts at the chaos creation step** — i.e., we expect chaos to land on the kind cluster but the scenario's load step will fail because the target has no app to load against. That's fine for verifying the cross-cluster Path.

```bash
./controlplane/bin/dlh run mysql-pod-delete --target kind --param vus=1 --param load_duration=10s --param chaos_duration=5s
```

Wait ~10s, then:

```bash
kubectl --context kind-dlh-target -n dlh-test-fw get schedules.chaos-mesh.org -l dlh.managed-by=controlplane
kubectl --context kind-dlh-target -n dlh-test-fw get schedules.chaos-mesh.org -l dlh.target=kind
```

Expected: A Schedule CR exists in the **kind** cluster (NOT in the framework cluster's chaos namespace). Both label filters return it.

- [ ] **Step 8: Verify the framework cluster has no chaos.**

```bash
kubectl --context minikube -n dlh-test-fw get schedules.chaos-mesh.org -l dlh.run-id=<the-run-id>
```

Expected: empty.

- [ ] **Step 9: Watch the watchdog reap when the workflow ends.**

```bash
sleep 30
kubectl --context kind-dlh-target -n dlh-test-fw get schedules.chaos-mesh.org -l dlh.managed-by=controlplane
```

Expected: empty (cleanup-chaos step OR watchdog reaped it).

- [ ] **Step 10: Fix any bugs that surfaced** with individual commits (`fix(controlplane): <one-line>`).

Common likely failures:
- kind's API server not reachable from inside minikube → network bridge issue. Document the workaround OR switch to a second minikube profile (`minikube start -p dlh-target`).
- chaos-mesh's webhook on kind rejects something → check `kubectl --context kind-dlh-target logs -n chaos-mesh deployment/chaos-controller-manager`.
- Watchdog deletes chaos before the wait step has finished → race because the workflow exits "Pending"->"Failed" quickly when the mysql load step finds no mysql. **Expected** for this smoke; not a bug.

- [ ] **Step 11: Cleanup.**

```bash
kill $PF 2>/dev/null || true
# Keep the kind cluster around for future debugging unless the smoke worked clean — easy to recreate.
# kind delete cluster --name dlh-target
```

---

## Task 24: CI extension + docs (FINDINGS + CLAUDE + README)

**Files:**
- Modify: `.github/workflows/ci.yml` (add a `go vet`/test pass on the new packages; no e2e — too heavy)
- Modify: `docs/FINDINGS.md`
- Modify: `CLAUDE.md`
- Modify: `README.md`

- [ ] **Step 1: Inspect existing CI** to see what already covers `internal/targets`.

```bash
grep -n "go test\|go vet" .github/workflows/ci.yml
```

If the existing job runs `go test ./...`, no change is needed — new packages are picked up automatically. If it iterates over a hard-coded list, extend it.

- [ ] **Step 2: Append Plan 17 section to docs/FINDINGS.md.**

Use Edit. Append after the existing last pitfall block:

```markdown

---

## Plan 17 — controlplane Phase D (remote targets) (2026-05-23)

### What landed

- `controlplane/internal/targets/` — Registry + Loader + Refresher (30s poll) + Probe.
- `controlplane/internal/chaos/remote.go` — RemoteChaosClient (per-target dynamic client built from kubeconfig Secret).
- `controlplane/internal/chaos/router.go` — Router picks Local vs Remote per call based on targetID. Existing Client-interface methods union across all clients (so watchdog reaps cross-cluster).
- `POST /api/runs` accepts optional `targetId`; `Run.target` populated end-to-end; manifest gains `runs/index/by-target/{target}/...` index.
- `GET /api/targets`, `GET /api/targets/{id}`, `POST /api/targets/{id}/test` (admin-only probe).
- UI Targets page + per-card TargetPicker on Scenarios.
- `dlh run --target` + `dlh runs ls --target`.
- 3 chaos WTs accept `target_id` parameter (default `""`) and forward to `/internal/chaos?targetID=...`.
- Empty `dlh-targets` ConfigMap shipped by the umbrella chart so a fresh install is fully Phase-C-compatible.
- Operator runbook + Argo CD Application example for installing chaos-mesh into a target cluster.

### Operational pitfalls discovered (record so Phase E doesn't re-learn)

1. **kind's API server is on `127.0.0.1:<port>`, unreachable from minikube.** Bridge via docker networks: `docker network connect kind minikube; docker network connect minikube dlh-target-control-plane`, then rewrite the kubeconfig's server URL to `https://<container-ip>:6443`. A second minikube profile (`minikube start -p dlh-target`) avoids the issue entirely and is the smoother test path.

2. **`kubectl create token` requires k8s 1.24+ and a token-issuing API.** Older clusters need a manually-created Secret of type kubernetes.io/service-account-token. We document the kubectl-token path; older clusters operators should swap.

3. **RBAC `resourceNames` doesn't wildcard.** We can't restrict `secrets get` to `dlh-target-*` — RBAC requires exact names. Controlplane Role now allows `get/list/watch` on all secrets in its namespace. Trade-off accepted because the controlplane namespace is platform-managed (no untrusted workload secrets).

4. **First Refresher tick must be synchronous.** Otherwise the controlplane starts serving requests before the registry is populated, and `/api/targets` briefly returns empty. The `Refresher.Run` calls `tick()` once before the ticker loop.

5. **`dyn.Resource(gvr).Namespace(ns).List` with an absent CRD returns an error, not empty.** We tolerate it in ListByRun + ListManaged by `continue`-ing past errors. The error path is acceptable because the absence of one chaos kind doesn't invalidate the others.

6. **Argo `http` template URL templating respects `&` in query strings.** Forwarding both `runID` and `targetID` works as-is: `?runID={{workflow.name}}&targetID={{inputs.parameters.target_id}}`. Empty target_id produces `&targetID=` which the controlplane handler treats as empty — local routing.

7. **Workflow argument default `value: ""`** is required by Argo for parameters without inputs. Scenarios that don't set `target_id` in their arguments inherit the empty default; the chaos WT forwards `""` which routes to LocalChaosClient.

### Carry-forward for Phase E

- OIDC device-code flow for `dlh login`.
- CI OIDC token exchange endpoint for GH Actions.
- Removal of `scripts/run-scenario.sh` entirely.
- Notification hooks (Slack/email on run completion) — interface stub already lives in Phase C's event-bus design.
```

- [ ] **Step 3: Append a Phase D subsection to CLAUDE.md** under the existing dlh-controlplane block:

```markdown

### Phase D additions (Plan 17)

- Registered remote targets live in the `dlh-targets` ConfigMap +
  per-target `dlh-target-<id>` Secrets (each holding `kubeconfig`).
- `POST /api/runs` accepts optional `targetId`; the Workflow gets
  `dlh.target` label + `target_id` workflow argument. Chaos WTs
  forward the latter via `?targetID=...` query in /internal/chaos calls.
- `chaos.Router` picks Local vs Remote per call. Empty targetID is
  the default — preserves Phase C behaviour.
- New endpoints: `GET /api/targets`, `GET /api/targets/{id}`,
  `POST /api/targets/{id}/test`.
- New UI page: Targets (read-only + test-connection button). New
  control in Scenarios cards: a TargetPicker dropdown.
- New CLI flag: `dlh run --target <id>` / `dlh runs ls --target <id>`.
- Operator runbook: `docs/operations/register-target.md`.
```

- [ ] **Step 4: Append Plan 17 row to README.md plan table.**

```markdown
| Plan 17 | `XXXXXXX` | dlh-controlplane Phase D (remote targets) — Target registry from ConfigMap+Secrets; RemoteChaosClient + Router; /api/targets + UI TargetsPage + TargetPicker; dlh run --target; chaos WTs forward target_id |
```

(`XXXXXXX` placeholder backfilled at merge time.)

- [ ] **Step 5: Commit.**

```bash
git add docs/FINDINGS.md CLAUDE.md README.md .github/workflows/ci.yml
git commit -m "docs: Plan 17 — Phase D FINDINGS + CLAUDE.md + README"
```

---

## Task 25: Final build + helm-lint + render verification

**Files:** None modified.

- [ ] **Step 1: Full local CI:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17/controlplane
go vet ./...
go test ./...
make ui-build
make cli
```

Expected: all green.

- [ ] **Step 2: Chart render + kubeconform:**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17
helm dependency update helm/dlh-test-fw 2>&1 | tail -3
helm lint helm/dlh-test-fw 2>&1 | tail -5
helm template dlh helm/dlh-test-fw > /tmp/rendered-plan17.yaml
kubeconform -skip CustomResourceDefinition -strict -summary \
  -schema-location default \
  -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
  /tmp/rendered-plan17.yaml controlplane/deploy/*.yaml 2>&1 | tail -3
```

Expected: 0 Invalid, 0 Errors.

- [ ] **Step 3: shellcheck on changed shell scripts (no changes expected this phase; verify nothing regressed):**

```bash
shellcheck -S error scripts/*.sh
```

No commit. Verification only.

---

## Task 26: Push branch + verify CI + merge to main

**Files:** None modified except README backfill.

- [ ] **Step 1: Push the feature branch + watch CI.**

```bash
cd /Users/allen/repo/dlh-test-fw-plan17
git push -u origin feat/plan17-controlplane-remote-targets
RUN_ID=$(gh run list --branch feat/plan17-controlplane-remote-targets --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch "$RUN_ID" --interval 30 || true
gh run view "$RUN_ID" --json conclusion -q .conclusion
```

Expected: `success`. If `controlplane` CI fails on a new test, fix on the feature branch + force-push? **No** — add a fix commit + push.

- [ ] **Step 2: Verify the chart Application would render cleanly (already covered by Task 25; this is a confidence check):**

```bash
grep -rn "REPLACE-" controlplane/deploy/ argocd/apps/dlh-controlplane.yaml docs/operations/register-target.md
```

Expected: Phase B/C placeholders (REPLACE-OIDC-ISSUER etc.) AND Phase D's REPLACE-CLUSTER in the chaos-mesh example. Document if needed.

- [ ] **Step 3: Merge with --no-ff.**

```bash
cd /Users/allen/repo/dlh-test-fw
git checkout main
git pull --ff-only origin main
git merge --no-ff feat/plan17-controlplane-remote-targets -m "Merge feat/plan17-controlplane-remote-targets: controlplane Phase D (remote targets)

Adds multi-cluster chaos support — controlplane learns about external
target clusters from an Argo-CD-synced ConfigMap + per-target kubeconfig
Secrets; chaos.Router picks LocalChaosClient vs RemoteChaosClient per
call; /internal/chaos accepts targetID query param; POST /api/runs
accepts optional targetId; Run.Target + Manifest.Target flow end-to-end
with a by-target index in MinIO; UI Targets page + TargetPicker;
dlh run --target. Chaos WTs forward target_id through their http steps.

Plan 17 of dlh-test-fw. See:
- docs/superpowers/specs/2026-05-21-dlh-controlplane-design.md
- docs/superpowers/plans/2026-05-23-01-controlplane-remote-targets.md
- docs/operations/register-target.md (operator runbook)

Phase E (auth polish + CI OIDC + run-scenario.sh removal) gets its
own plan."
```

- [ ] **Step 4: Backfill README plan hash.**

```bash
MERGE_HASH=$(git log --first-parent --format=%h -1)
sed -i "" "s|| Plan 17 | \`XXXXXXX\`|| Plan 17 | \`$MERGE_HASH\`|" README.md
git add README.md
git commit -m "docs(readme): backfill Plan 17 merge hash"
```

- [ ] **Step 5: Push main + verify CI:**

```bash
git push origin main
sleep 10
gh run list --branch main --limit 1
```

- [ ] **Step 6: Worktree cleanup:**

```bash
git worktree remove ../dlh-test-fw-plan17
git branch -d feat/plan17-controlplane-remote-targets
git push origin --delete feat/plan17-controlplane-remote-targets
```

- [ ] **Step 7: Final verify:**

```bash
git log --first-parent --oneline -5
ls controlplane/internal/targets/
grep "^| Plan 17" README.md
git worktree list
```

---

## Done

Plan 17 lands cross-cluster chaos. After merge: a target cluster can be registered via PR; UI shows it; `dlh run --target` injects chaos into the target's chaos-mesh; the watchdog reaps cross-cluster orphans; the manifest layer carries `target` through every layer. Phase E (auth polish + CI OIDC) gets its own plan when you're ready.
