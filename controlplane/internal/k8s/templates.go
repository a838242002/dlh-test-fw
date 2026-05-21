package k8s

import (
	"context"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TemplateLister abstracts WorkflowTemplate retrieval for handler tests.
type TemplateLister interface {
	ListTemplates(ctx context.Context) ([]wfv1.WorkflowTemplate, error)
	GetTemplate(ctx context.Context, name string) (*wfv1.WorkflowTemplate, error)
}

type templateLister struct {
	c         *Clients
	namespace string
}

// NewTemplateLister returns a TemplateLister scoped to the given namespace.
func NewTemplateLister(c *Clients, namespace string) TemplateLister {
	return &templateLister{c: c, namespace: namespace}
}

func (l *templateLister) ListTemplates(ctx context.Context) ([]wfv1.WorkflowTemplate, error) {
	list, err := l.c.Argo.ArgoprojV1alpha1().WorkflowTemplates(l.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (l *templateLister) GetTemplate(ctx context.Context, name string) (*wfv1.WorkflowTemplate, error) {
	return l.c.Argo.ArgoprojV1alpha1().WorkflowTemplates(l.namespace).Get(ctx, name, metav1.GetOptions{})
}
