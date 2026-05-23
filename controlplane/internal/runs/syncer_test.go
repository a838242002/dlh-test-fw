package runs

import (
	"context"
	"errors"
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

// fakeReportSource implements ReportSource for testing Score population.
type fakeReportSource struct {
	data map[string]any
	err  error
}

func (f *fakeReportSource) Read(_ context.Context, _ string) (map[string]any, error) {
	return f.data, f.err
}

func TestReadScore_PassVerdict(t *testing.T) {
	src := &fakeReportSource{data: map[string]any{"overall": true}}
	v, ok := readScore(context.Background(), src, "wf-1")
	if !ok {
		t.Fatal("expected ok=true for overall:true")
	}
	if v != 1.0 {
		t.Errorf("score = %v, want 1.0", v)
	}
}

func TestReadScore_FailVerdict(t *testing.T) {
	src := &fakeReportSource{data: map[string]any{"overall": false}}
	v, ok := readScore(context.Background(), src, "wf-1")
	if !ok {
		t.Fatal("expected ok=true for overall:false")
	}
	if v != 0.0 {
		t.Errorf("score = %v, want 0.0", v)
	}
}

func TestReadScore_NilSrc(t *testing.T) {
	_, ok := readScore(context.Background(), nil, "wf-1")
	if ok {
		t.Error("expected ok=false for nil source")
	}
}

func TestReadScore_ReadError(t *testing.T) {
	src := &fakeReportSource{err: errors.New("minio unavailable")}
	_, ok := readScore(context.Background(), src, "wf-1")
	if ok {
		t.Error("expected ok=false when Read returns error")
	}
}

func TestReadScore_MissingOverallField(t *testing.T) {
	src := &fakeReportSource{data: map[string]any{"thresholds": []any{}}}
	_, ok := readScore(context.Background(), src, "wf-1")
	if ok {
		t.Error("expected ok=false when overall field is absent")
	}
}

func TestSyncer_PopulatesScoreOnSucceeded(t *testing.T) {
	src := &fakeEventSource{ch: make(chan k8s.WorkflowEvent, 2)}
	sink := &captureSink{}
	reports := &fakeReportSource{data: map[string]any{"overall": true}}
	s := &Syncer{Source: src, Manifests: sink, Reports: reports}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "score-run",
			Labels:            map[string]string{"dlh.run-id": "score-run", "dlh.scenario": "mysql-pod-delete"},
			CreationTimestamp: metav1.NewTime(time.Now()),
		},
		Status: wfv1.WorkflowStatus{
			Phase:      "Succeeded",
			FinishedAt: metav1.NewTime(time.Now()),
		},
	}
	src.ch <- k8s.WorkflowEvent{Type: "MODIFIED", Workflow: wf}

	time.Sleep(200 * time.Millisecond)
	got := sink.snapshot()
	if len(got) == 0 {
		t.Fatal("no manifest written")
	}
	last := got[len(got)-1]
	if last.Score == nil {
		t.Fatal("Score should be non-nil for a Succeeded workflow with a verdict report")
	}
	if *last.Score != 1.0 {
		t.Errorf("Score = %v, want 1.0", *last.Score)
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
