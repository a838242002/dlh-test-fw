package runs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSubmit_CreatesWorkflowWithTemplateRef(t *testing.T) {
	ns := "dlh-test-fw"
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns, Labels: map[string]string{"dlh.category": "scenario"}},
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

func TestSubmit_PriorityOverrideStampsWorkflow(t *testing.T) {
	ns := "dlh-test-fw"
	baked := int32(100)
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns, Labels: map[string]string{"dlh.category": "scenario"}},
		Spec:       wfv1.WorkflowSpec{Priority: &baked},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	s := &Submitter{Argo: argo, Namespace: ns}

	// explicit override wins
	p := 500
	res, err := s.Submit(context.Background(), SubmitRequest{ScenarioID: "mysql-pod-delete", Priority: &p})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res.RunID, metav1.GetOptions{})
	if got.Spec.Priority == nil || *got.Spec.Priority != 500 {
		t.Errorf("override priority: got %v want 500", got.Spec.Priority)
	}
}

func TestSubmit_PriorityFallsBackToBaked(t *testing.T) {
	ns := "dlh-test-fw"
	baked := int32(100)
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns, Labels: map[string]string{"dlh.category": "scenario"}},
		Spec:       wfv1.WorkflowSpec{Priority: &baked},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	s := &Submitter{Argo: argo, Namespace: ns}

	res, err := s.Submit(context.Background(), SubmitRequest{ScenarioID: "mysql-pod-delete"}) // no override
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res.RunID, metav1.GetOptions{})
	if got.Spec.Priority == nil || *got.Spec.Priority != 100 {
		t.Errorf("baked priority: got %v want 100", got.Spec.Priority)
	}
}

type fakeDefaults struct{ m map[string]int }

func (f fakeDefaults) Get(_ context.Context, scenario string) (int, bool, error) {
	v, ok := f.m[scenario]
	return v, ok, nil
}

func TestSubmit_PriorityUsesScenarioDefault(t *testing.T) {
	ns := "dlh-test-fw"
	baked := int32(100)
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns, Labels: map[string]string{"dlh.category": "scenario"}},
		Spec:       wfv1.WorkflowSpec{Priority: &baked},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	s := &Submitter{Argo: argo, Namespace: ns, Defaults: fakeDefaults{m: map[string]int{"mysql-pod-delete": 300}}}

	// no explicit override → scenario default (300) wins over baked (100)
	res, err := s.Submit(context.Background(), SubmitRequest{ScenarioID: "mysql-pod-delete"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res.RunID, metav1.GetOptions{})
	if got.Spec.Priority == nil || *got.Spec.Priority != 300 {
		t.Errorf("scenario-default priority: got %v want 300", got.Spec.Priority)
	}

	// explicit override still wins over the scenario default.
	// Sleep 1 s so the second runID (second-precision timestamp) is distinct.
	time.Sleep(time.Second)
	p := 500
	res2, err2 := s.Submit(context.Background(), SubmitRequest{ScenarioID: "mysql-pod-delete", Priority: &p})
	if err2 != nil {
		t.Fatalf("Submit override: %v", err2)
	}
	got2, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), res2.RunID, metav1.GetOptions{})
	if got2.Spec.Priority == nil || *got2.Spec.Priority != 500 {
		t.Errorf("override over default: got %v want 500", got2.Spec.Priority)
	}
}

func TestSubmit_WithTargetID(t *testing.T) {
	ns := "dlh-test-fw"
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: ns, Labels: map[string]string{"dlh.category": "scenario"}},
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

func TestSubmit_RejectsNonScenario(t *testing.T) {
	ns := "dlh-test-fw"
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "chaos-kafka-broker-partition", Namespace: ns,
			Labels: map[string]string{"dlh.category": "chaos"}},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	s := &Submitter{Argo: argo, Namespace: ns}
	_, err := s.Submit(context.Background(), SubmitRequest{ScenarioID: "chaos-kafka-broker-partition"})
	if !errors.Is(err, ErrNotScenario) {
		t.Fatalf("expected ErrNotScenario, got %v", err)
	}
}

// A template with no labels at all (nil Labels map) must also be rejected —
// reading a nil map yields "" which is != "scenario".
func TestSubmit_RejectsTemplateWithoutLabels(t *testing.T) {
	ns := "dlh-test-fw"
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "fixture-minio-load-mysql", Namespace: ns},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	s := &Submitter{Argo: argo, Namespace: ns}
	_, err := s.Submit(context.Background(), SubmitRequest{ScenarioID: "fixture-minio-load-mysql"})
	if !errors.Is(err, ErrNotScenario) {
		t.Fatalf("expected ErrNotScenario, got %v", err)
	}
}
