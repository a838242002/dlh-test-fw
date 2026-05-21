package k8s

import (
	"context"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func newFakeClients(objs ...runtime.Object) *Clients {
	return &Clients{Argo: wfake.NewSimpleClientset(objs...)}
}

func TestListTemplates(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"},
	}
	c := newFakeClients(tmpl)
	l := NewTemplateLister(c, "dlh-test-fw")

	got, err := l.ListTemplates(context.Background())
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(got) != 1 || got[0].Name != "mysql-pod-delete" {
		t.Errorf("unexpected templates: %+v", got)
	}
}

func TestGetTemplate(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "kafka-broker-partition", Namespace: "dlh-test-fw"},
	}
	c := newFakeClients(tmpl)
	l := NewTemplateLister(c, "dlh-test-fw")
	got, err := l.GetTemplate(context.Background(), "kafka-broker-partition")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.Name != "kafka-broker-partition" {
		t.Errorf("got: %+v", got)
	}
}
