package runs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
)

// ManifestSink is the write side of the manifest store. ManifestWriter
// satisfies it; tests can substitute a fake.
type ManifestSink interface {
	Write(ctx context.Context, m Manifest) error
}

// WorkflowEventSource is satisfied by k8s.WorkflowLister via Subscribe().
type WorkflowEventSource interface {
	Subscribe() (<-chan k8s.WorkflowEvent, func())
}

// ReportSource is satisfied by *minio.ReportReader.
type ReportSource interface {
	Read(ctx context.Context, workflowName string) (map[string]any, error)
}

// Syncer subscribes to a WorkflowEventSource and writes manifests on
// every Workflow event. Coalesces by last-written status to avoid
// MinIO write spam on informer resync events.
type Syncer struct {
	Source    WorkflowEventSource
	Manifests ManifestSink
	Reports   ReportSource

	mu   sync.Mutex
	last map[string]string // runID -> last written status
}

// Run blocks until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context) {
	if s.last == nil {
		s.last = map[string]string{}
	}
	ch, cancel := s.Source.Subscribe()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			s.handle(ctx, ev)
		}
	}
}

func (s *Syncer) handle(ctx context.Context, ev k8s.WorkflowEvent) {
	if ev.Workflow == nil {
		return
	}
	wf := ev.Workflow
	runID, ok := wf.Labels["dlh.run-id"]
	if !ok {
		runID = wf.Name
	}
	status := string(wf.Status.Phase)
	if status == "" {
		status = "Pending"
	}

	s.mu.Lock()
	prev := s.last[runID]
	s.last[runID] = status
	s.mu.Unlock()

	if prev == status && ev.Type != "DELETED" {
		return // coalesce informer resync noise
	}

	m := Manifest{
		RunID:        runID,
		Scenario:     wf.Labels["dlh.scenario"],
		WorkflowName: wf.Name,
		Status:       status,
		StartedAt:    wf.CreationTimestamp.Time,
	}
	if !wf.Status.FinishedAt.IsZero() {
		t := wf.Status.FinishedAt.Time
		m.FinishedAt = &t
		if score, ok := readScore(ctx, s.Reports, wf.Name); ok {
			m.Score = &score
		}
	}
	if err := s.Manifests.Write(ctx, m); err != nil {
		slog.Warn("manifest write failed", "runID", runID, "err", err)
	}
}

// readScore pulls a numeric score from report.json's top-level `score` field.
func readScore(ctx context.Context, src ReportSource, workflowName string) (float64, bool) {
	if src == nil {
		return 0, false
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := src.Read(cctx, workflowName)
	if err != nil || r == nil {
		return 0, false
	}
	if v, ok := r["score"].(float64); ok {
		return v, true
	}
	return 0, false
}
