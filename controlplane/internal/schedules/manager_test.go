package schedules

import (
	"context"
	"strings"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCreate_HappyPath(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"},
	}
	argo := wfake.NewSimpleClientset(tmpl)
	m := &Manager{Argo: argo, Namespace: "dlh-test-fw"}
	got, err := m.Create(context.Background(), CreateRequest{
		Name:       "nightly-mysql",
		ScenarioID: "mysql-pod-delete",
		Cron:       "0 2 * * *",
		CreatedBy:  "tester",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Name != "nightly-mysql" {
		t.Errorf("name: %q", got.Name)
	}
	if got.Spec.Schedule != "0 2 * * *" {
		t.Errorf("schedule: %q", got.Spec.Schedule)
	}
	if got.Spec.WorkflowSpec.WorkflowTemplateRef == nil || got.Spec.WorkflowSpec.WorkflowTemplateRef.Name != "mysql-pod-delete" {
		t.Errorf("templateRef: %+v", got.Spec.WorkflowSpec.WorkflowTemplateRef)
	}
	foundTargetID := false
	for _, p := range got.Spec.WorkflowSpec.Arguments.Parameters {
		if p.Name == "target_id" {
			foundTargetID = true
		}
	}
	if !foundTargetID {
		t.Errorf("target_id parameter not appended: %+v", got.Spec.WorkflowSpec.Arguments.Parameters)
	}
	if got.Spec.WorkflowMetadata == nil || got.Spec.WorkflowMetadata.Labels["dlh.scenario"] != "mysql-pod-delete" {
		t.Errorf("workflowMetadata.labels: %+v", got.Spec.WorkflowMetadata)
	}
}

func TestCreate_WithTarget(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"}}
	argo := wfake.NewSimpleClientset(tmpl)
	m := &Manager{Argo: argo, Namespace: "dlh-test-fw"}
	got, _ := m.Create(context.Background(), CreateRequest{
		Name:       "nightly-mysql-staging",
		ScenarioID: "mysql-pod-delete",
		TargetID:   "staging-mysql",
		Cron:       "0 2 * * *",
	})
	if got.Spec.WorkflowMetadata.Labels["dlh.target"] != "staging-mysql" {
		t.Errorf("dlh.target label missing: %+v", got.Spec.WorkflowMetadata.Labels)
	}
}

func TestCreate_UnknownScenario(t *testing.T) {
	argo := wfake.NewSimpleClientset()
	m := &Manager{Argo: argo, Namespace: "dlh-test-fw"}
	_, err := m.Create(context.Background(), CreateRequest{
		Name: "x", ScenarioID: "nope", Cron: "0 * * * *",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestCreate_RejectsEmpty(t *testing.T) {
	m := &Manager{Argo: wfake.NewSimpleClientset(), Namespace: "dlh-test-fw"}
	cases := []CreateRequest{
		{Name: "", ScenarioID: "x", Cron: "0 * * * *"},
		{Name: "x", ScenarioID: "", Cron: "0 * * * *"},
		{Name: "x", ScenarioID: "y", Cron: ""},
	}
	for i, c := range cases {
		if _, err := m.Create(context.Background(), c); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestValidateName(t *testing.T) {
	cases := []struct {
		in     string
		wantOK bool
	}{
		{"foo", true},
		{"foo-bar", true},
		{"a.b", true},
		{"", false},
		{"-foo", false},
		{"foo-", false},
		{"Foo", false},
		{"under_score", false},
	}
	for _, c := range cases {
		err := validateName(c.in)
		if (err == nil) != c.wantOK {
			t.Errorf("validateName(%q): err=%v wantOK=%v", c.in, err, c.wantOK)
		}
	}
}
