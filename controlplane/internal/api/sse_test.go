package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
)

type controllableWorkflows struct {
	events chan k8s.WorkflowEvent
	wf     *wfv1.Workflow
}

func (c *controllableWorkflows) List(_ k8s.WorkflowFilter) ([]*wfv1.Workflow, error) {
	return nil, nil
}
func (c *controllableWorkflows) Get(name string) (*wfv1.Workflow, error) {
	if c.wf != nil && c.wf.Name == name {
		return c.wf, nil
	}
	return nil, errFakeNotFound{}
}
func (c *controllableWorkflows) Subscribe() (<-chan k8s.WorkflowEvent, func()) {
	return c.events, func() {}
}

func TestSSE_EmitsSnapshotThenEvent(t *testing.T) {
	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1"},
		Status:     wfv1.WorkflowStatus{Phase: "Running"},
	}
	src := &controllableWorkflows{events: make(chan k8s.WorkflowEvent, 1), wf: wf}
	sseH := &SSEHandler{Workflows: src}

	r := chi.NewRouter()
	r.Get("/api/runs/{id}/events", sseH.Handle)
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/runs/run-1/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	go func() {
		time.Sleep(250 * time.Millisecond)
		newWf := wf.DeepCopy()
		newWf.Status.Phase = "Succeeded"
		src.events <- k8s.WorkflowEvent{Type: "MODIFIED", Workflow: newWf}
	}()

	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "event: snapshot") {
		t.Errorf("expected snapshot event in %q", body)
	}
	// Read more for the MODIFIED event.
	n2, _ := resp.Body.Read(buf)
	body2 := string(buf[:n2])
	if !strings.Contains(body2, "event: MODIFIED") {
		t.Errorf("expected MODIFIED event in %q", body2)
	}
}
