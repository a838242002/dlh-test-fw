package k8s

import (
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFilter_Scenario(t *testing.T) {
	now := metav1.Now()
	items := []*wfv1.Workflow{
		{ObjectMeta: metav1.ObjectMeta{Name: "a", Labels: map[string]string{"dlh.scenario": "mysql-pod-delete"}, CreationTimestamp: now}},
		{ObjectMeta: metav1.ObjectMeta{Name: "b", Labels: map[string]string{"dlh.scenario": "kafka-broker-partition"}, CreationTimestamp: now}},
	}
	got := filterWorkflows(items, WorkflowFilter{Scenario: "mysql-pod-delete"})
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("got %+v", got)
	}
}

func TestFilter_Since(t *testing.T) {
	cutoff := time.Now().Add(-1 * time.Hour)
	items := []*wfv1.Workflow{
		{ObjectMeta: metav1.ObjectMeta{Name: "old", CreationTimestamp: metav1.NewTime(cutoff.Add(-2 * time.Hour))}},
		{ObjectMeta: metav1.ObjectMeta{Name: "new", CreationTimestamp: metav1.NewTime(cutoff.Add(time.Hour))}},
	}
	got := filterWorkflows(items, WorkflowFilter{Since: &cutoff})
	if len(got) != 1 || got[0].Name != "new" {
		t.Errorf("got %+v", got)
	}
}

func TestFilter_StatusAndLimit(t *testing.T) {
	now := metav1.Now()
	items := []*wfv1.Workflow{
		{ObjectMeta: metav1.ObjectMeta{Name: "1", CreationTimestamp: now}, Status: wfv1.WorkflowStatus{Phase: "Running"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "2", CreationTimestamp: now}, Status: wfv1.WorkflowStatus{Phase: "Running"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "3", CreationTimestamp: now}, Status: wfv1.WorkflowStatus{Phase: "Succeeded"}},
	}
	got := filterWorkflows(items, WorkflowFilter{Status: "Running", Limit: 1})
	if len(got) != 1 {
		t.Fatalf("expected 1 (limit), got %d", len(got))
	}
	if got[0].Status.Phase != "Running" {
		t.Errorf("phase: %v", got[0].Status.Phase)
	}
}
