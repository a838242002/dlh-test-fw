package api

import (
	"context"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
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
