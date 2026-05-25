package api

import (
	"context"
	"strings"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
	"github.com/dlh/dlh-test-fw/controlplane/internal/queue"
	"github.com/dlh/dlh-test-fw/controlplane/internal/runs"
)

// fakeTemplates implements k8s.TemplateLister backed by an in-memory slice.
type fakeTemplates struct {
	items []wfv1.WorkflowTemplate
}

func (f *fakeTemplates) ListTemplates(_ context.Context) ([]wfv1.WorkflowTemplate, error) {
	return f.items, nil
}

func (f *fakeTemplates) GetTemplate(_ context.Context, name string) (*wfv1.WorkflowTemplate, error) {
	for i := range f.items {
		if f.items[i].Name == name {
			return &f.items[i], nil
		}
	}
	return nil, errFakeNotFound{}
}

// fakeWorkflows implements k8s.WorkflowLister backed by an in-memory slice.
type fakeWorkflows struct {
	items []*wfv1.Workflow
}

func (f *fakeWorkflows) List(_ k8s.WorkflowFilter) ([]*wfv1.Workflow, error) {
	return f.items, nil
}

func (f *fakeWorkflows) Get(name string) (*wfv1.Workflow, error) {
	for _, w := range f.items {
		if w.Name == name {
			return w, nil
		}
	}
	return nil, errFakeNotFound{}
}

func (f *fakeWorkflows) Subscribe() (<-chan k8s.WorkflowEvent, func()) {
	ch := make(chan k8s.WorkflowEvent)
	return ch, func() { close(ch) }
}

type errFakeNotFound struct{}

func (errFakeNotFound) Error() string { return "not found" }

func TestListScenarios(t *testing.T) {
	deps := &Deps{
		Templates: &fakeTemplates{items: []wfv1.WorkflowTemplate{
			{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete"}},
		}},
	}
	h := &Handlers{deps: deps}
	resp, err := h.ListScenarios(context.Background(), gen.ListScenariosRequestObject{})
	if err != nil {
		t.Fatalf("ListScenarios: %v", err)
	}
	out, ok := resp.(gen.ListScenarios200JSONResponse)
	if !ok {
		t.Fatalf("unexpected response type: %T", resp)
	}
	if len(out.Items) != 1 || out.Items[0].Id != "mysql-pod-delete" {
		t.Errorf("got %+v", out.Items)
	}
}

func TestListRuns(t *testing.T) {
	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "run-1",
			Labels: map[string]string{"dlh.scenario": "mysql-pod-delete"},
		},
		Status: wfv1.WorkflowStatus{Phase: "Running"},
	}
	deps := &Deps{
		Templates: &fakeTemplates{},
		Workflows: &fakeWorkflows{items: []*wfv1.Workflow{wf}},
	}
	h := &Handlers{deps: deps}
	resp, err := h.ListRuns(context.Background(), gen.ListRunsRequestObject{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	out, ok := resp.(gen.ListRuns200JSONResponse)
	if !ok {
		t.Fatalf("unexpected response type: %T", resp)
	}
	if len(out.Items) != 1 || out.Items[0].Id != "run-1" {
		t.Errorf("got %+v", out.Items)
	}
}

func TestCreateRun_Submits(t *testing.T) {
	ns := "dlh-test-fw"
	tmpl := &wfv1.WorkflowTemplate{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns}}
	argo := wfake.NewSimpleClientset(tmpl)

	deps := &Deps{
		Templates: &fakeTemplates{items: []wfv1.WorkflowTemplate{*tmpl}},
		Submitter: &runs.Submitter{Argo: argo, Namespace: ns},
		Manifests: &runs.ManifestWriter{Client: nil, Bucket: "artifacts"}, // nil-client → Write no-ops
	}
	h := &Handlers{deps: deps}

	scenarioID := "mysql-pod-delete"
	req := gen.CreateRunRequestObject{Body: &gen.CreateRunRequest{ScenarioId: scenarioID}}
	resp, err := h.CreateRun(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	out, ok := resp.(gen.CreateRun202JSONResponse)
	if !ok {
		t.Fatalf("unexpected response type: %T", resp)
	}
	if !strings.HasPrefix(out.Id, "mysql-pod-delete-") {
		t.Errorf("RunID: %q", out.Id)
	}
}

func TestCreateRun_404OnUnknownScenario(t *testing.T) {
	argo := wfake.NewSimpleClientset()
	deps := &Deps{
		Templates: &fakeTemplates{},
		Submitter: &runs.Submitter{Argo: argo, Namespace: "dlh-test-fw"},
		Manifests: &runs.ManifestWriter{Bucket: "artifacts"},
	}
	h := &Handlers{deps: deps}
	resp, err := h.CreateRun(context.Background(), gen.CreateRunRequestObject{
		Body: &gen.CreateRunRequest{ScenarioId: "does-not-exist"},
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, ok := resp.(gen.CreateRun404Response); !ok {
		t.Fatalf("expected 404, got %T", resp)
	}
}

func TestGetAuthInfo_PopulatesFromDeps(t *testing.T) {
	deps := &Deps{AuthInfo: AuthInfoConfig{
		OIDCIssuer:   "https://issuer.example.com",
		OIDCClientID: "client-x",
		CIAudience:   "aud-y",
		AuthDisabled: false,
	}}
	h := &Handlers{deps: deps}
	resp, err := h.GetAuthInfo(context.Background(), gen.GetAuthInfoRequestObject{})
	if err != nil {
		t.Fatalf("GetAuthInfo: %v", err)
	}
	out, ok := resp.(gen.GetAuthInfo200JSONResponse)
	if !ok {
		t.Fatalf("response type: %T", resp)
	}
	if out.OidcIssuer != "https://issuer.example.com" || out.OidcClientId != "client-x" {
		t.Errorf("info: %+v", out)
	}
}

func TestCreateRun_ForwardsPriority(t *testing.T) {
	ns := "dlh-test-fw"
	baked := int32(100)
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns},
		Spec:       wfv1.WorkflowSpec{Priority: &baked},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	deps := &Deps{
		Templates: &fakeTemplates{items: []wfv1.WorkflowTemplate{*tmpl}},
		Submitter: &runs.Submitter{Argo: argo, Namespace: ns},
		Manifests: &runs.ManifestWriter{Client: nil, Bucket: "artifacts"},
	}
	h := &Handlers{deps: deps}

	prio := 500
	req := gen.CreateRunRequestObject{Body: &gen.CreateRunRequest{ScenarioId: "mysql-pod-delete", Priority: &prio}}
	resp, err := h.CreateRun(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	out := resp.(gen.CreateRun202JSONResponse)
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), out.Id, metav1.GetOptions{})
	if got.Spec.Priority == nil || *got.Spec.Priority != 500 {
		t.Errorf("workflow priority: got %v want 500", got.Spec.Priority)
	}
}

// fakeLocks implements LocksReader.
type fakeLocks struct{ keys []queue.LockKey }

func (f *fakeLocks) Keys(_ context.Context) ([]queue.LockKey, error) { return f.keys, nil }

type fakePriorities struct{ m map[string]int }

func (f *fakePriorities) All(_ context.Context) (map[string]int, error) { return f.m, nil }
func (f *fakePriorities) Get(_ context.Context, s string) (int, bool, error) {
	v, ok := f.m[s]
	return v, ok, nil
}
func (f *fakePriorities) Set(_ context.Context, s string, p int) error {
	if f.m == nil {
		f.m = map[string]int{}
	}
	f.m[s] = p
	return nil
}

func TestPutAndGetScenarioPriorities(t *testing.T) {
	ns := "dlh-test-fw"
	baked := int32(100)
	tmpl := wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns},
		Spec:       wfv1.WorkflowSpec{Priority: &baked},
	}
	fp := &fakePriorities{m: map[string]int{}}
	deps := &Deps{
		Templates:  &fakeTemplates{items: []wfv1.WorkflowTemplate{tmpl}},
		Priorities: fp,
	}
	h := &Handlers{deps: deps}

	// PUT override 500
	prio := 500
	putResp, err := h.PutScenarioPriority(context.Background(), gen.PutScenarioPriorityRequestObject{
		Id:   "mysql-pod-delete",
		Body: &gen.PutScenarioPriorityJSONRequestBody{Priority: prio},
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	po := putResp.(gen.PutScenarioPriority200JSONResponse)
	if po.Override == nil || *po.Override != 500 || po.Effective != 500 || po.Baked != 100 {
		t.Errorf("put result: %+v", po)
	}

	// GET reflects it
	getResp, _ := h.GetScenarioPriorities(context.Background(), gen.GetScenarioPrioritiesRequestObject{})
	go200 := getResp.(gen.GetScenarioPriorities200JSONResponse)
	if len(go200.Items) != 1 || go200.Items[0].Scenario != "mysql-pod-delete" || go200.Items[0].Effective != 500 {
		t.Errorf("get items: %+v", go200.Items)
	}

	// PUT unknown scenario → 404
	r404, _ := h.PutScenarioPriority(context.Background(), gen.PutScenarioPriorityRequestObject{
		Id: "nope", Body: &gen.PutScenarioPriorityJSONRequestBody{Priority: 1},
	})
	if _, ok := r404.(gen.PutScenarioPriority404Response); !ok {
		t.Errorf("expected 404 for unknown scenario, got %T", r404)
	}
}

func TestGetQueue_GroupsAndOrders(t *testing.T) {
	t0 := metav1.Now()
	mk := func(name, scenario, phase string, prio int32) *wfv1.Workflow {
		return &wfv1.Workflow{
			ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: t0,
				Labels: map[string]string{"dlh.scenario": scenario}},
			Spec:   wfv1.WorkflowSpec{Priority: &prio},
			Status: wfv1.WorkflowStatus{Phase: wfv1.WorkflowPhase(phase)},
		}
	}
	deps := &Deps{
		Workflows: &fakeWorkflows{items: []*wfv1.Workflow{
			mk("m-run", "mysql-pod-delete", "Running", 100),
			mk("m-pend", "mysql-pod-delete", "Pending", 500),
		}},
		Locks: &fakeLocks{keys: []queue.LockKey{{Key: "mysql", Slots: 1}, {Key: "kafka", Slots: 1}}},
	}
	h := &Handlers{deps: deps}
	resp, err := h.GetQueue(context.Background(), gen.GetQueueRequestObject{})
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	out := resp.(gen.GetQueue200JSONResponse)
	if len(out.Lanes) != 2 {
		t.Fatalf("expected 2 lanes, got %d", len(out.Lanes))
	}
	if out.Lanes[0].Key != "mysql" || len(out.Lanes[0].Running) != 1 || len(out.Lanes[0].Pending) != 1 {
		t.Errorf("mysql lane: %+v", out.Lanes[0])
	}
}

func TestReprioritizeRun_Statuses(t *testing.T) {
	ns := "dlh-test-fw"
	pending := &wfv1.Workflow{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: ns},
		Status: wfv1.WorkflowStatus{Phase: wfv1.WorkflowPending}}
	running := &wfv1.Workflow{ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: ns},
		Status: wfv1.WorkflowStatus{Phase: wfv1.WorkflowRunning}}
	argo := wfake.NewSimpleClientset(pending, running)
	deps := &Deps{
		Submitter: &runs.Submitter{Argo: argo, Namespace: ns},
		Workflows: &fakeWorkflows{items: []*wfv1.Workflow{pending, running}},
	}
	h := &Handlers{deps: deps}

	// pending → 202
	r202, err := h.ReprioritizeRun(context.Background(), gen.ReprioritizeRunRequestObject{
		Id: "p", Body: &gen.ReprioritizeRunJSONRequestBody{Priority: 500}})
	if err != nil {
		t.Fatalf("Reprioritize pending: %v", err)
	}
	if _, ok := r202.(gen.ReprioritizeRun202Response); !ok {
		t.Errorf("pending: got %T want 202", r202)
	}

	// running → 409
	r409, _ := h.ReprioritizeRun(context.Background(), gen.ReprioritizeRunRequestObject{
		Id: "r", Body: &gen.ReprioritizeRunJSONRequestBody{Priority: 500}})
	if _, ok := r409.(gen.ReprioritizeRun409Response); !ok {
		t.Errorf("running: got %T want 409", r409)
	}

	// unknown → 404
	r404, _ := h.ReprioritizeRun(context.Background(), gen.ReprioritizeRunRequestObject{
		Id: "nope", Body: &gen.ReprioritizeRunJSONRequestBody{Priority: 500}})
	if _, ok := r404.(gen.ReprioritizeRun404Response); !ok {
		t.Errorf("unknown: got %T want 404", r404)
	}
}
