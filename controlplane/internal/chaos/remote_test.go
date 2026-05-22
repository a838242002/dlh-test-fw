package chaos

import (
	"context"
	"testing"
)

func TestRemoteChaosClient_NoRestConfig(t *testing.T) {
	r := &RemoteChaosClient{Namespace: "dlh-test-fw", TargetID: "x"}
	_, err := r.Create(context.Background(), "run-1", Resource{
		APIVersion: "chaos-mesh.org/v1alpha1", Kind: "Schedule",
		Metadata: map[string]interface{}{"name": "x"},
		Spec:     map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error when RestConfig is nil")
	}
}
