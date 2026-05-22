package runs

import (
	"context"
	"sync"
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
)

type fakeEventSource struct {
	ch chan k8s.WorkflowEvent
}

func (f *fakeEventSource) Subscribe() (<-chan k8s.WorkflowEvent, func()) {
	return f.ch, func() {}
}

type captureSink struct {
	mu  sync.Mutex
	got []Manifest
}

func (c *captureSink) Write(_ context.Context, m Manifest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, m)
	return nil
}

func (c *captureSink) snapshot() []Manifest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Manifest, len(c.got))
	copy(out, c.got)
	return out
}

func TestSyncer_WritesOnStatusChange(t *testing.T) {
	src := &fakeEventSource{ch: make(chan k8s.WorkflowEvent, 3)}
	sink := &captureSink{}
	s := &Syncer{Source: src, Manifests: sink}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "run-1",
			Labels:            map[string]string{"dlh.run-id": "run-1", "dlh.scenario": "mysql-pod-delete"},
			CreationTimestamp: metav1.NewTime(time.Now()),
		},
		Status: wfv1.WorkflowStatus{Phase: "Running"},
	}
	src.ch <- k8s.WorkflowEvent{Type: "ADDED", Workflow: wf}
	src.ch <- k8s.WorkflowEvent{Type: "MODIFIED", Workflow: wf}

	wf2 := wf.DeepCopy()
	wf2.Status.Phase = "Succeeded"
	wf2.Status.FinishedAt = metav1.NewTime(time.Now())
	src.ch <- k8s.WorkflowEvent{Type: "MODIFIED", Workflow: wf2}

	time.Sleep(400 * time.Millisecond)
	got := sink.snapshot()
	if len(got) < 2 {
		t.Fatalf("expected at least 2 writes, got %d: %+v", len(got), got)
	}
	if got[len(got)-1].Status != "Succeeded" {
		t.Errorf("last status: %q", got[len(got)-1].Status)
	}
}

func TestSyncer_CoalescesIdenticalEvents(t *testing.T) {
	src := &fakeEventSource{ch: make(chan k8s.WorkflowEvent, 10)}
	sink := &captureSink{}
	s := &Syncer{Source: src, Manifests: sink}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "run-2",
			Labels:            map[string]string{"dlh.run-id": "run-2"},
			CreationTimestamp: metav1.NewTime(time.Now()),
		},
		Status: wfv1.WorkflowStatus{Phase: "Running"},
	}
	// 5 identical events should collapse to 1 write.
	for i := 0; i < 5; i++ {
		src.ch <- k8s.WorkflowEvent{Type: "MODIFIED", Workflow: wf}
	}
	time.Sleep(400 * time.Millisecond)
	got := sink.snapshot()
	if len(got) != 1 {
		t.Errorf("expected exactly 1 write after coalesce, got %d", len(got))
	}
}

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
