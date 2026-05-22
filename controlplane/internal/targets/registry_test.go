package targets

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRegistry_GetAndList(t *testing.T) {
	r := NewRegistry()
	if r.Get("nope") != nil {
		t.Errorf("Get on empty registry should be nil")
	}
	r.Replace(map[string]*LoadedTarget{
		"a": {Target: Target{ID: "a"}},
		"b": {Target: Target{ID: "b"}},
	})
	if r.Get("a") == nil || r.Get("b") == nil {
		t.Errorf("Get missed populated targets")
	}
	if r.Get("c") != nil {
		t.Errorf("Get on unknown id should be nil")
	}
	if len(r.List()) != 2 {
		t.Errorf("List length: %d", len(r.List()))
	}
}

func TestLoader_EmptyConfigMap(t *testing.T) {
	ns := "dlh-test-fw"
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "dlh-targets", Namespace: ns},
		Data:       map[string]string{},
	}
	client := fake.NewSimpleClientset(cm)
	l := &Loader{Client: client, Namespace: ns}
	got, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty registry, got %d entries", len(got))
	}
}

func TestLoader_TargetEntries(t *testing.T) {
	ns := "dlh-test-fw"
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "dlh-targets", Namespace: ns},
		Data: map[string]string{
			"targets.yaml": `
targets:
  - id: staging-mysql
    kubeconfigSecret: dlh-target-staging-mysql
    allowedTargetTypes: [mysql]
    namespace: dlh-test-fw
  - id: preprod-kafka
    kubeconfigSecret: dlh-target-preprod-kafka
    allowedTargetTypes: [kafka]
`,
		},
	}
	// Minimal valid kubeconfig — must parse via clientcmd.RESTConfigFromKubeConfig.
	validKubeconfig := []byte(`apiVersion: v1
kind: Config
clusters:
  - name: t
    cluster:
      server: https://example.com
contexts:
  - name: t
    context:
      cluster: t
      user: t
current-context: t
users:
  - name: t
    user: {}`)
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "dlh-target-staging-mysql", Namespace: ns},
		Data:       map[string][]byte{"kubeconfig": validKubeconfig},
	}
	client := fake.NewSimpleClientset(cm, sec)
	l := &Loader{Client: client, Namespace: ns}

	got, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 targets, got %d", len(got))
	}
	if got["staging-mysql"] == nil || got["staging-mysql"].RestConfig == nil {
		t.Errorf("staging-mysql should have RestConfig: %+v", got["staging-mysql"])
	}
	// preprod-kafka has no secret in the fake client → tolerated, RestConfig nil
	if got["preprod-kafka"] == nil || got["preprod-kafka"].RestConfig != nil {
		t.Errorf("preprod-kafka should be present with nil RestConfig: %+v", got["preprod-kafka"])
	}
	if got["staging-mysql"].DisplayName != "staging-mysql" {
		t.Errorf("DisplayName default: %q", got["staging-mysql"].DisplayName)
	}
}
