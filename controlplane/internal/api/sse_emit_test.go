package api

// Verifies the SSE handler's EVENT-EMISSION behavior (distinct from
// sse_routing_test.go which only proves the real handler is reached, and
// sse_auth_test.go which only covers token extraction):
//   1. On connect it emits an initial `snapshot` event carrying the run's phase.
//   2. It emits MODIFIED events (with the new phase) for the requested run id.
//   3. It FILTERS OUT events for other run ids.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/go-chi/chi/v5"

	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
)

// emitLister is a WorkflowLister whose Subscribe() channel the test drives, and
// whose Get() returns a single seeded workflow for the snapshot.
type emitLister struct {
	wf *wfv1.Workflow
	ch chan k8s.WorkflowEvent
}

func (f *emitLister) List(_ k8s.WorkflowFilter) ([]*wfv1.Workflow, error) { return nil, nil }
func (f *emitLister) Get(name string) (*wfv1.Workflow, error) {
	if f.wf != nil && f.wf.Name == name {
		return f.wf, nil
	}
	return nil, errFakeNotFound{}
}
func (f *emitLister) Subscribe() (<-chan k8s.WorkflowEvent, func()) { return f.ch, func() {} }

// flushRecorder is a thread-safe http.ResponseWriter + http.Flusher so the
// streaming handler (run in a goroutine) can be observed without data races.
type flushRecorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
	hdr http.Header
}

func newFlushRecorder() *flushRecorder { return &flushRecorder{hdr: http.Header{}} }
func (r *flushRecorder) Header() http.Header { return r.hdr }
func (r *flushRecorder) WriteHeader(int)     {}
func (r *flushRecorder) Flush()              {}
func (r *flushRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}
func (r *flushRecorder) body() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}
func (r *flushRecorder) waitFor(sub string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if strings.Contains(r.body(), sub) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func phaseWF(name, phase string) *wfv1.Workflow {
	w := &wfv1.Workflow{}
	w.Name = name
	w.Status.Phase = wfv1.WorkflowPhase(phase)
	return w
}

func TestSSEHandler_EmitsSnapshotAndFilteredEvents(t *testing.T) {
	const runID = "wf-emit-1"
	fl := &emitLister{wf: phaseWF(runID, "Running"), ch: make(chan k8s.WorkflowEvent, 8)}
	h := &SSEHandler{Workflows: fl}

	rec := newFlushRecorder()
	req := httptest.NewRequest("GET", "/api/runs/"+runID+"/events", nil)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", runID)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	done := make(chan struct{})
	go func() { h.Handle(rec, req); close(done) }()

	// 1) initial snapshot with the current phase
	if !rec.waitFor("event: snapshot", time.Second) || !strings.Contains(rec.body(), `"phase":"Running"`) {
		t.Fatalf("expected snapshot event with phase Running; body=%q", rec.body())
	}

	// 2) MODIFIED event for our run id is emitted with the new phase
	fl.ch <- k8s.WorkflowEvent{Type: "MODIFIED", Workflow: phaseWF(runID, "Succeeded")}
	if !rec.waitFor("event: MODIFIED", time.Second) || !strings.Contains(rec.body(), `"phase":"Succeeded"`) {
		t.Fatalf("expected MODIFIED event with phase Succeeded; body=%q", rec.body())
	}

	// 3) an event for a DIFFERENT run id must be filtered out
	fl.ch <- k8s.WorkflowEvent{Type: "MODIFIED", Workflow: phaseWF("some-other-run", "Failed")}
	time.Sleep(150 * time.Millisecond)
	if strings.Contains(rec.body(), `"phase":"Failed"`) || strings.Contains(rec.body(), "some-other-run") {
		t.Fatalf("event for a different run id leaked into the stream; body=%q", rec.body())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after context cancel")
	}
}
