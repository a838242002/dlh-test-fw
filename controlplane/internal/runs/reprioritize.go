package runs

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

// ErrNotPending is returned when a reprioritize targets a run that is not
// queued (only Pending runs can be re-ordered; Argo fixes order at admission).
var ErrNotPending = errors.New("run is not pending")

// Reprioritize patches a pending workflow's spec.priority so Argo releases it
// in the new order. Returns ErrNotPending if the run is not Pending.
func (s *Submitter) Reprioritize(ctx context.Context, runID string, priority int) error {
	wf, err := s.Argo.ArgoprojV1alpha1().Workflows(s.Namespace).Get(ctx, runID, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get workflow %q: %w", runID, err)
	}
	if wf.Status.Phase != wfv1.WorkflowPending && wf.Status.Phase != "" {
		return ErrNotPending
	}
	patch := []byte(fmt.Sprintf(`{"spec":{"priority":%d}}`, int32(priority)))
	_, err = s.Argo.ArgoprojV1alpha1().Workflows(s.Namespace).Patch(
		ctx, runID, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}
