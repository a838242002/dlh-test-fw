package runs

import (
	"context"
	"errors"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
)

func TestReprioritize_PendingOnly(t *testing.T) {
	ns := "dlh-test-fw"
	pending := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: ns},
		Status:     wfv1.WorkflowStatus{Phase: wfv1.WorkflowPending},
	}
	running := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: ns},
		Status:     wfv1.WorkflowStatus{Phase: wfv1.WorkflowRunning},
	}
	argo := wfake.NewSimpleClientset(pending, running)
	s := &Submitter{Argo: argo, Namespace: ns}

	// running → ErrNotPending
	if err := s.Reprioritize(context.Background(), "r", 500); !errors.Is(err, ErrNotPending) {
		t.Errorf("running reprioritize: got %v want ErrNotPending", err)
	}

	// pending → patches spec.priority
	if err := s.Reprioritize(context.Background(), "p", 500); err != nil {
		t.Fatalf("pending reprioritize: %v", err)
	}
	got, _ := argo.ArgoprojV1alpha1().Workflows(ns).Get(context.Background(), "p", metav1.GetOptions{})
	if got.Spec.Priority == nil || *got.Spec.Priority != 500 {
		t.Errorf("patched priority: got %v want 500", got.Spec.Priority)
	}
}
