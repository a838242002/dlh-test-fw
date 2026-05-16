package chaosresult

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

var gvr = schema.GroupVersionResource{Group: "litmuschaos.io", Version: "v1alpha1", Resource: "chaosresults"}

func mkCR(verdict string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "litmuschaos.io/v1alpha1",
		"kind":       "ChaosResult",
		"metadata":   map[string]any{"name": "cr1", "namespace": "dlh-test-fw"},
		"status": map[string]any{
			"experimentStatus": map[string]any{"verdict": verdict},
		},
	}}
}

func TestGetVerdictPass(t *testing.T) {
	scheme := runtime.NewScheme()
	dc := dynamicfake.NewSimpleDynamicClient(scheme, mkCR("Pass"))
	c := &Client{Dyn: dc, GVR: gvr, Namespace: "dlh-test-fw"}
	v, err := c.GetVerdict(context.Background(), "cr1", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if v != "Pass" {
		t.Errorf("got %q want Pass", v)
	}
}

func TestGetVerdictAwaitedTimesOut(t *testing.T) {
	scheme := runtime.NewScheme()
	dc := dynamicfake.NewSimpleDynamicClient(scheme, mkCR("Awaited"))
	c := &Client{Dyn: dc, GVR: gvr, Namespace: "dlh-test-fw", PollInterval: 50 * time.Millisecond}
	_, err := c.GetVerdict(context.Background(), "cr1", 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
