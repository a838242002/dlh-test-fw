package runs

import (
	"context"
	"strings"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSubmit_CreatesWorkflowWithTemplateRef(t *testing.T) {
	ns := "dlh-test-fw"
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns},
	}
	argo := wfake.NewSimpleClientset(tmpl)

	s := &Submitter{Argo: argo, Namespace: ns}
	res, err := s.Submit(context.Background(), SubmitRequest{
		ScenarioID: "mysql-pod-delete",
		Parameters: map[string]string{"vus": "20"},
		CreatedBy:  "tester",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !strings.HasPrefix(res.RunID, "mysql-pod-delete-") {
		t.Errorf("RunID: %q", res.RunID)
	}
	got, err := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res.RunID, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.WorkflowTemplateRef == nil || got.Spec.WorkflowTemplateRef.Name != "mysql-pod-delete" {
		t.Errorf("templateRef wrong: %+v", got.Spec.WorkflowTemplateRef)
	}
	if got.Labels["dlh.scenario"] != "mysql-pod-delete" {
		t.Errorf("label: %v", got.Labels)
	}
	// Expect vus + target_id (always appended).
	if len(got.Spec.Arguments.Parameters) != 2 {
		t.Errorf("expected 2 params (vus + target_id), got: %+v", got.Spec.Arguments.Parameters)
	}
	foundVus := false
	for _, p := range got.Spec.Arguments.Parameters {
		if p.Name == "vus" {
			foundVus = true
		}
	}
	if !foundVus {
		t.Errorf("vus param missing: %+v", got.Spec.Arguments.Parameters)
	}
}

func TestSubmit_404ForUnknownScenario(t *testing.T) {
	argo := wfake.NewSimpleClientset()
	s := &Submitter{Argo: argo, Namespace: "dlh-test-fw"}
	_, err := s.Submit(context.Background(), SubmitRequest{ScenarioID: "nope"})
	if err == nil {
		t.Fatal("expected error for unknown scenario")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error text: %v", err)
	}
}

func TestSubmit_EmptyScenarioRejected(t *testing.T) {
	s := &Submitter{Argo: wfake.NewSimpleClientset(), Namespace: "dlh-test-fw"}
	_, err := s.Submit(context.Background(), SubmitRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSubmit_WithTargetID(t *testing.T) {
	ns := "dlh-test-fw"
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	s := &Submitter{Argo: argo, Namespace: ns}
	res, err := s.Submit(context.Background(), SubmitRequest{
		ScenarioID: "mysql-pod-delete",
		TargetID:   "staging-mysql",
		CreatedBy:  "tester",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res.TargetID != "staging-mysql" {
		t.Errorf("TargetID echo: %q", res.TargetID)
	}
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res.RunID, metav1.GetOptions{})
	if got.Labels["dlh.target"] != "staging-mysql" {
		t.Errorf("dlh.target label: %v", got.Labels)
	}
	foundTargetArg := false
	for _, p := range got.Spec.Arguments.Parameters {
		if p.Name == "target_id" && p.Value != nil && p.Value.String() == "staging-mysql" {
			foundTargetArg = true
			break
		}
	}
	if !foundTargetArg {
		t.Errorf("target_id parameter not propagated: %+v", got.Spec.Arguments.Parameters)
	}
}
