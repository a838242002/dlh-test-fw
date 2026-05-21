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
	if len(got.Spec.Arguments.Parameters) != 1 || got.Spec.Arguments.Parameters[0].Name != "vus" {
		t.Errorf("params: %+v", got.Spec.Arguments.Parameters)
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
