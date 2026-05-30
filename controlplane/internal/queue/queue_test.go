package queue

import (
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// semName builds Argo's fully-qualified semaphore name for a bare lock key,
// matching the live format "dlh-test-fw/ConfigMap/dlh-scenario-locks/<key>".
func semName(key string) string {
	return "dlh-test-fw/ConfigMap/dlh-scenario-locks/" + key
}

// wf builds a workflow fixture. holding/waiting are bare lock keys (e.g.
// "mysql"); each is expanded to a fully-qualified semaphore name. phase sets
// the overall workflow phase — deliberately decoupled from lock ownership so
// tests can prove classification ignores phase.
func wf(name, scenario, phase string, prio int32, created time.Time, holding, waiting []string) *wfv1.Workflow {
	w := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(created),
			Labels:            map[string]string{"dlh.scenario": scenario},
		},
		Spec:   wfv1.WorkflowSpec{Priority: &prio},
		Status: wfv1.WorkflowStatus{Phase: wfv1.WorkflowPhase(phase)},
	}
	if len(holding) > 0 || len(waiting) > 0 {
		sem := &wfv1.SemaphoreStatus{}
		for _, k := range holding {
			sem.Holding = append(sem.Holding, wfv1.SemaphoreHolding{Semaphore: semName(k), Holders: []string{name}})
		}
		for _, k := range waiting {
			// Argo records the *blocker* in waiting[].holders, not self — the
			// implementation must not rely on holders contents.
			sem.Waiting = append(sem.Waiting, wfv1.SemaphoreHolding{Semaphore: semName(k), Holders: []string{"some-other-holder"}})
		}
		w.Status.Synchronization = &wfv1.SynchronizationStatus{Semaphore: sem}
	}
	return w
}

func TestBuildLanes_HolderIsRunning(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-run", "mysql-pod-delete", "Running", 100, t0, []string{"mysql"}, nil),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 1 || lanes[0].Running[0].ID != "m-run" {
		t.Errorf("holder should be Running: %+v", lanes[0])
	}
	if len(lanes[0].Pending) != 0 {
		t.Errorf("no pending expected: %+v", lanes[0].Pending)
	}
}

func TestBuildLanes_WaiterIsQueued(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-run", "mysql-pod-delete", "Running", 100, t0, []string{"mysql"}, nil),
		wf("m-wait", "mysql-pod-delete", "Pending", 100, t0.Add(time.Minute), nil, []string{"mysql"}),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 1 || lanes[0].Running[0].ID != "m-run" {
		t.Errorf("running: %+v", lanes[0].Running)
	}
	if len(lanes[0].Pending) != 1 || lanes[0].Pending[0].ID != "m-wait" {
		t.Errorf("pending: %+v", lanes[0].Pending)
	}
}

// Regression for the 2/1 bug: two phase=Running workflows, but only one holds
// the lock. The other (here phase=Running too, but in .waiting) must be Queued.
func TestBuildLanes_OverSubscriptionGuard(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("k-hold", "kafka-broker-partition", "Running", 100, t0, []string{"kafka"}, nil),
		wf("k-wait", "chaos-kafka-broker-partition", "Running", 100, t0.Add(time.Minute), nil, []string{"kafka"}),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "kafka", Slots: 1}})
	if len(lanes[0].Running) != 1 {
		t.Fatalf("exactly one holder expected, got %d: %+v", len(lanes[0].Running), lanes[0].Running)
	}
	if lanes[0].Running[0].ID != "k-hold" {
		t.Errorf("wrong holder: %+v", lanes[0].Running)
	}
	if len(lanes[0].Pending) != 1 || lanes[0].Pending[0].ID != "k-wait" {
		t.Errorf("waiter should be Queued: %+v", lanes[0].Pending)
	}
}

// A phase=Running workflow with no synchronization (pre-gate or post-release)
// contends for nothing and must appear in no lane.
func TestBuildLanes_NoSyncIsAbsent(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-prep", "mysql-pod-delete", "Running", 100, t0, nil, nil),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 0 || len(lanes[0].Pending) != 0 {
		t.Errorf("non-contending workflow must be absent: %+v", lanes[0])
	}
}

func TestBuildLanes_MultiLaneIsolation(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-run", "mysql-pod-delete", "Running", 100, t0, []string{"mysql"}, nil),
		wf("k-run", "kafka-broker-partition", "Running", 100, t0, []string{"kafka"}, nil),
	}
	keys := []LockKey{{Key: "mysql", Slots: 1}, {Key: "kafka", Slots: 1}, {Key: "doris", Slots: 1}}
	lanes := BuildLanes(wfs, keys)
	if len(lanes) != 3 {
		t.Fatalf("expected 3 lanes, got %d", len(lanes))
	}
	if lanes[0].Key != "mysql" || len(lanes[0].Running) != 1 || lanes[0].Running[0].ID != "m-run" {
		t.Errorf("mysql lane: %+v", lanes[0])
	}
	if lanes[1].Key != "kafka" || len(lanes[1].Running) != 1 || lanes[1].Running[0].ID != "k-run" {
		t.Errorf("kafka lane: %+v", lanes[1])
	}
}

func TestBuildLanes_IdleLane(t *testing.T) {
	wfs := []*wfv1.Workflow{}
	lanes := BuildLanes(wfs, []LockKey{{Key: "doris", Slots: 1}})
	if len(lanes) != 1 || lanes[0].Key != "doris" {
		t.Fatalf("expected doris lane, got %+v", lanes)
	}
	if len(lanes[0].Running) != 0 || len(lanes[0].Pending) != 0 {
		t.Errorf("idle lane should be empty: %+v", lanes[0])
	}
}

func TestBuildLanes_PendingOrder(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-run", "mysql-pod-delete", "Running", 100, t0, []string{"mysql"}, nil),
		wf("m-lowprio-old", "mysql-pod-delete", "Pending", 100, t0.Add(1*time.Minute), nil, []string{"mysql"}),
		wf("m-highprio-new", "mysql-pod-delete", "Pending", 500, t0.Add(2*time.Minute), nil, []string{"mysql"}),
		wf("m-lowprio-newer", "mysql-pod-delete", "Pending", 100, t0.Add(3*time.Minute), nil, []string{"mysql"}),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	p := lanes[0].Pending
	// higher priority first; among equal priority, oldest first
	if len(p) != 3 {
		t.Fatalf("expected 3 pending, got %d: %+v", len(p), p)
	}
	if p[0].ID != "m-highprio-new" {
		t.Errorf("p[0] should be highest priority: %+v", p)
	}
	if p[1].ID != "m-lowprio-old" || p[2].ID != "m-lowprio-newer" {
		t.Errorf("equal-priority entries must be oldest-first: %+v", p)
	}
}

func TestBuildLanes_UnknownKeyIgnored(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("x-run", "some-scenario", "Running", 100, t0, []string{"redis"}, nil),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 0 || len(lanes[0].Pending) != 0 {
		t.Errorf("unknown-key holder must be ignored: %+v", lanes[0])
	}
}

func TestBuildLanes_TerminalIgnored(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		// stale synchronization on a finished workflow must not leak in
		wf("m-done", "mysql-pod-delete", "Succeeded", 100, t0, []string{"mysql"}, nil),
		wf("m-failed", "mysql-pod-delete", "Failed", 100, t0, []string{"mysql"}, nil),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 0 || len(lanes[0].Pending) != 0 {
		t.Errorf("terminal workflows must not appear: %+v", lanes[0])
	}
}

func TestLockKey(t *testing.T) {
	cases := map[string]string{
		"dlh-test-fw/ConfigMap/dlh-scenario-locks/mysql": "mysql",
		"mysql":      "mysql",
		"":           "",
		"trailing/":  "",
		"a/b/c/doris": "doris",
	}
	for in, want := range cases {
		if got := lockKey(in); got != want {
			t.Errorf("lockKey(%q) = %q, want %q", in, got, want)
		}
	}
}
