package runs

import (
	"context"
	"fmt"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Submitter creates new Workflow CRs from WorkflowTemplate refs.
type Submitter struct {
	Argo      wfclient.Interface
	Namespace string
}

// SubmitRequest is the inbound payload (one-step removed from the HTTP DTO).
type SubmitRequest struct {
	ScenarioID string
	Parameters map[string]string
	CreatedBy  string // OIDC subject
}

// SubmitResult is what we return to the caller.
type SubmitResult struct {
	RunID     string
	StartedAt time.Time
}

// Submit creates the Workflow CR. RunID format mirrors run-scenario.sh:
// "<scenarioID>-YYYYMMDD-HHMMSS".
func (s *Submitter) Submit(ctx context.Context, req SubmitRequest) (*SubmitResult, error) {
	if req.ScenarioID == "" {
		return nil, fmt.Errorf("scenarioId is required")
	}
	// Verify the template exists; this becomes 404 to the API caller.
	if _, err := s.Argo.ArgoprojV1alpha1().WorkflowTemplates(s.Namespace).Get(ctx, req.ScenarioID, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("scenario %q not found: %w", req.ScenarioID, err)
		}
		return nil, fmt.Errorf("get workflowtemplate %q: %w", req.ScenarioID, err)
	}

	now := time.Now().UTC()
	runID := fmt.Sprintf("%s-%s", req.ScenarioID, now.Format("20060102-150405"))

	params := make([]wfv1.Parameter, 0, len(req.Parameters))
	for k, v := range req.Parameters {
		val := wfv1.AnyString(v)
		params = append(params, wfv1.Parameter{Name: k, Value: &val})
	}

	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runID,
			Namespace: s.Namespace,
			Labels: map[string]string{
				"dlh.scenario": req.ScenarioID,
				"dlh.run-id":   runID,
			},
			Annotations: map[string]string{
				"dlh.created-by": req.CreatedBy,
			},
		},
		Spec: wfv1.WorkflowSpec{
			WorkflowTemplateRef: &wfv1.WorkflowTemplateRef{Name: req.ScenarioID},
			Arguments:           wfv1.Arguments{Parameters: params},
		},
	}

	created, err := s.Argo.ArgoprojV1alpha1().Workflows(s.Namespace).Create(ctx, wf, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create workflow: %w", err)
	}
	return &SubmitResult{RunID: created.Name, StartedAt: created.CreationTimestamp.Time}, nil
}
