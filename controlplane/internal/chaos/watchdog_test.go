package chaos

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeChaosForWatchdog struct {
	mu      sync.Mutex
	managed map[string][]Ref
	deleted []Ref
}

func (f *fakeChaosForWatchdog) Create(_ context.Context, _ string, _ Resource) (Ref, error) {
	return Ref{}, nil
}
func (f *fakeChaosForWatchdog) Delete(_ context.Context, r Ref) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, r)
	return nil
}
func (f *fakeChaosForWatchdog) DeleteByRun(_ context.Context, _ string) error { return nil }
func (f *fakeChaosForWatchdog) ListByRun(_ context.Context, runID string) ([]Ref, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.managed[runID], nil
}
func (f *fakeChaosForWatchdog) ListManaged(_ context.Context) (map[string][]Ref, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Return a snapshot copy so the watchdog tick + Delete don't race with the test.
	out := map[string][]Ref{}
	for k, v := range f.managed {
		out[k] = append([]Ref(nil), v...)
	}
	return out, nil
}

func (f *fakeChaosForWatchdog) deletedSnapshot() []Ref {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Ref, len(f.deleted))
	copy(out, f.deleted)
	return out
}

func TestWatchdog_ReapsTerminalRuns(t *testing.T) {
	fc := &fakeChaosForWatchdog{
		managed: map[string][]Ref{
			"run-running":  {{Name: "sched-x", Namespace: "dlh-test-fw"}},
			"run-finished": {{Name: "sched-y", Namespace: "dlh-test-fw"}},
		},
	}
	w := &Watchdog{
		Chaos: fc,
		RunsTerminal: RunsTerminalCheckerFunc(func(runID string) bool {
			return runID == "run-finished"
		}),
		Interval: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	deleted := fc.deletedSnapshot()
	// Should have deleted sched-y multiple times (one per tick — Delete is idempotent
	// per LocalChaosClient.Delete tolerating NotFound). Verify at least one delete of sched-y
	// and no deletes of sched-x.
	if len(deleted) == 0 {
		t.Fatalf("expected at least one deletion, got 0")
	}
	for _, r := range deleted {
		if r.Name != "sched-y" {
			t.Errorf("unexpected deletion: %+v", r)
		}
	}
}

func TestWatchdog_DefaultInterval(t *testing.T) {
	w := &Watchdog{
		Chaos:        &fakeChaosForWatchdog{managed: map[string][]Ref{}},
		RunsTerminal: RunsTerminalCheckerFunc(func(_ string) bool { return false }),
		// Interval=0 — should default to 30s
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w.Run(ctx)
	if w.Interval != 30*time.Second {
		t.Errorf("default interval: got %v, want 30s", w.Interval)
	}
}
