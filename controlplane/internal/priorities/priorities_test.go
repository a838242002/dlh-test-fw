package priorities

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func newStore(data map[string]string) *Store {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "dlh-scenario-priorities", Namespace: "dlh-test-fw"},
		Data:       data,
	}
	return &Store{Client: fake.NewSimpleClientset(cm), Namespace: "dlh-test-fw", Name: "dlh-scenario-priorities"}
}

func TestStore_GetSet(t *testing.T) {
	s := newStore(map[string]string{"mysql-pod-delete": "200"})
	ctx := context.Background()

	if v, ok, _ := s.Get(ctx, "mysql-pod-delete"); !ok || v != 200 {
		t.Fatalf("Get existing: got %d ok=%v want 200 true", v, ok)
	}
	if _, ok, _ := s.Get(ctx, "kafka-broker-partition"); ok {
		t.Fatal("Get missing: expected ok=false")
	}

	if err := s.Set(ctx, "kafka-broker-partition", 500); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, ok, _ := s.Get(ctx, "kafka-broker-partition"); !ok || v != 500 {
		t.Errorf("Get after Set: got %d ok=%v want 500 true", v, ok)
	}
}

func TestStore_All(t *testing.T) {
	s := newStore(map[string]string{"a": "10", "b": "not-an-int"})
	all, err := s.All(context.Background())
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if all["a"] != 10 {
		t.Errorf("a: got %d want 10", all["a"])
	}
	if _, ok := all["b"]; ok {
		t.Error("non-int values must be skipped")
	}
}
