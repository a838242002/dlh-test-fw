package model

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

func TestScenarioFromTemplate_DerivedDescriptionUsesSloValue(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{}
	tmpl.Name = "kafka-broker-partition"
	v := wfv1.AnyString("broker-partition")
	tmpl.Spec.Arguments.Parameters = []wfv1.Parameter{
		{Name: "slo_name", Value: &v},
	}
	s := ScenarioFromTemplate(tmpl)
	if s.Description == nil {
		t.Fatal("description is nil")
	}
	want := "broker-partition chaos on a kafka target, evaluated against the broker-partition SLO."
	if *s.Description != want {
		t.Fatalf("description = %q, want %q", *s.Description, want)
	}
}

func TestRunDetailFromWorkflow_StepsSortedByStart(t *testing.T) {
	base := metav1.Now().Time
	wf := &wfv1.Workflow{}
	wf.Name = "wf-x"
	wf.Status.Nodes = wfv1.Nodes{
		"c": {DisplayName: "verdict", Phase: "Succeeded", StartedAt: metav1.NewTime(base.Add(3 * time.Minute))},
		"a": {DisplayName: "prep-slo", Phase: "Succeeded", StartedAt: metav1.NewTime(base)},
		"b": {DisplayName: "load", Phase: "Succeeded", StartedAt: metav1.NewTime(base.Add(20 * time.Second))},
	}
	d := RunDetailFromWorkflow(wf)
	if d.Steps == nil || len(*d.Steps) != 3 {
		t.Fatalf("want 3 steps, got %v", d.Steps)
	}
	got := []string{(*d.Steps)[0].Name, (*d.Steps)[1].Name, (*d.Steps)[2].Name}
	want := []string{"prep-slo", "load", "verdict"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step order = %v, want %v", got, want)
		}
	}
}

func TestRunFromWorkflow_Priority(t *testing.T) {
	p := int32(200)
	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete-20260101-000000",
			Labels: map[string]string{"dlh.scenario": "mysql-pod-delete"}},
		Spec: wfv1.WorkflowSpec{Priority: &p},
	}
	r := RunFromWorkflow(wf)
	if r.Priority == nil || *r.Priority != 200 {
		t.Errorf("priority: got %v want 200", r.Priority)
	}

	// no priority → nil
	wf2 := &wfv1.Workflow{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	if RunFromWorkflow(wf2).Priority != nil {
		t.Error("expected nil priority for workflow with no spec.priority")
	}
}
