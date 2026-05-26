package queue

import (
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func wf(name, scenario, phase string, prio int32, created time.Time) *wfv1.Workflow {
	return &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(created),
			Labels:            map[string]string{"dlh.scenario": scenario},
		},
		Spec:   wfv1.WorkflowSpec{Priority: &prio},
		Status: wfv1.WorkflowStatus{Phase: wfv1.WorkflowPhase(phase)},
	}
}

func TestBuildLanes_GroupsRunningAndOrdersPending(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-run", "mysql-pod-delete", "Running", 100, t0),
		wf("m-lowprio-old", "mysql-pod-delete", "Pending", 100, t0.Add(1*time.Minute)),
		wf("m-highprio-new", "mysql-pod-delete", "Pending", 500, t0.Add(2*time.Minute)),
		wf("k-run", "kafka-broker-partition", "Running", 100, t0),
	}
	keys := []LockKey{{Key: "mysql", Slots: 1}, {Key: "kafka", Slots: 1}, {Key: "doris", Slots: 1}}

	lanes := BuildLanes(wfs, keys)

	if len(lanes) != 3 {
		t.Fatalf("expected 3 lanes, got %d", len(lanes))
	}
	mysql := lanes[0]
	if mysql.Key != "mysql" || mysql.Slots != 1 {
		t.Fatalf("lane[0] = %+v", mysql)
	}
	if len(mysql.Running) != 1 || mysql.Running[0].ID != "m-run" {
		t.Errorf("mysql running: %+v", mysql.Running)
	}
	// higher priority first even though it was submitted later
	if len(mysql.Pending) != 2 || mysql.Pending[0].ID != "m-highprio-new" || mysql.Pending[1].ID != "m-lowprio-old" {
		t.Errorf("mysql pending order: %+v", mysql.Pending)
	}
	// doris lane is idle (present but empty)
	if lanes[2].Key != "doris" || len(lanes[2].Running) != 0 || len(lanes[2].Pending) != 0 {
		t.Errorf("doris lane should be idle: %+v", lanes[2])
	}
}

func TestBuildLanes_IgnoresTerminalWorkflows(t *testing.T) {
	t0 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	wfs := []*wfv1.Workflow{
		wf("m-done", "mysql-pod-delete", "Succeeded", 100, t0),
		wf("m-failed", "mysql-pod-delete", "Failed", 100, t0),
	}
	lanes := BuildLanes(wfs, []LockKey{{Key: "mysql", Slots: 1}})
	if len(lanes[0].Running) != 0 || len(lanes[0].Pending) != 0 {
		t.Errorf("terminal workflows must not appear: %+v", lanes[0])
	}
}
