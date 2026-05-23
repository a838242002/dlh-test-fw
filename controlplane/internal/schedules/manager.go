// Package schedules wraps Argo's CronWorkflow CRD as a controlplane
// "Schedule" resource. Mirrors runs.Submitter's shape so firing
// Workflows inherit the dlh.scenario + dlh.target labels that the
// existing Workflow informer + Syncer already understand.
package schedules

import (
	"context"
	"fmt"
	"strings"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Manager owns CronWorkflow lifecycle: create, list, get, delete, pause/resume.
type Manager struct {
	Argo      wfclient.Interface
	Namespace string
}

// CreateRequest is the inbound payload (one-step removed from the HTTP DTO).
type CreateRequest struct {
	Name       string            // user-supplied schedule id; must be a valid k8s name
	ScenarioID string            // WorkflowTemplate name
	TargetID   string            // empty = local
	Cron       string            // e.g. "*/15 * * * *"
	Timezone   string            // e.g. "Asia/Tokyo"; empty = UTC
	Parameters map[string]string // optional WT param overrides
	CreatedBy  string            // OIDC subject (annotation only)
}

// Create builds + applies a CronWorkflow CR. Returns the created object.
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*wfv1.CronWorkflow, error) {
	if err := validateName(req.Name); err != nil {
		return nil, err
	}
	if req.ScenarioID == "" {
		return nil, fmt.Errorf("scenarioId is required")
	}
	if req.Cron == "" {
		return nil, fmt.Errorf("cron is required")
	}
	// Verify the scenario WorkflowTemplate exists.
	if _, err := m.Argo.ArgoprojV1alpha1().WorkflowTemplates(m.Namespace).Get(ctx, req.ScenarioID, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("scenario %q not found: %w", req.ScenarioID, err)
		}
		return nil, fmt.Errorf("get workflowtemplate: %w", err)
	}

	// Build workflow params: user overrides + target_id (always present).
	params := make([]wfv1.Parameter, 0, len(req.Parameters)+1)
	for k, v := range req.Parameters {
		val := wfv1.AnyString(v)
		params = append(params, wfv1.Parameter{Name: k, Value: &val})
	}
	tidVal := wfv1.AnyString(req.TargetID)
	params = append(params, wfv1.Parameter{Name: "target_id", Value: &tidVal})

	// Labels propagate to child Workflows via workflowMetadata.
	wfLabels := map[string]string{
		"dlh.scenario": req.ScenarioID,
	}
	if req.TargetID != "" {
		wfLabels["dlh.target"] = req.TargetID
	}

	cron := &wfv1.CronWorkflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: m.Namespace,
			Labels: map[string]string{
				"dlh.scenario": req.ScenarioID,
				"dlh.schedule": req.Name,
			},
			Annotations: map[string]string{
				"dlh.created-by": req.CreatedBy,
			},
		},
		Spec: wfv1.CronWorkflowSpec{
			Schedule:          req.Cron,
			Timezone:          req.Timezone,
			ConcurrencyPolicy: wfv1.ForbidConcurrent,
			WorkflowSpec: wfv1.WorkflowSpec{
				WorkflowTemplateRef: &wfv1.WorkflowTemplateRef{Name: req.ScenarioID},
				Arguments:           wfv1.Arguments{Parameters: params},
				ServiceAccountName:  "argo-workflow",
			},
			WorkflowMetadata: &metav1.ObjectMeta{
				Labels: wfLabels,
			},
		},
	}
	created, err := m.Argo.ArgoprojV1alpha1().CronWorkflows(m.Namespace).Create(ctx, cron, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("schedule %q already exists", req.Name)
		}
		return nil, fmt.Errorf("create cronworkflow: %w", err)
	}
	return created, nil
}

// validateName rejects anything that wouldn't be a valid k8s resource name.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("schedule name is required")
	}
	if len(name) > 253 {
		return fmt.Errorf("schedule name too long (>253 chars)")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.':
		default:
			return fmt.Errorf("schedule name must be lowercase alphanumeric + '-' + '.'")
		}
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("schedule name cannot start or end with '-'")
	}
	return nil
}
