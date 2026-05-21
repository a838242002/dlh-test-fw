package chaos

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
)

func newDynFake() *LocalChaosClient {
	scheme := runtime.NewScheme()
	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "schedules"}:    "ScheduleList",
		{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "podchaos"}:     "PodChaosList",
		{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "networkchaos"}: "NetworkChaosList",
	}
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind)
	return &LocalChaosClient{Dyn: dyn, Namespace: "dlh-test-fw"}
}

func TestLocalChaosClient_CreateAndDelete(t *testing.T) {
	c := newDynFake()
	res := Resource{
		APIVersion: "chaos-mesh.org/v1alpha1",
		Kind:       "Schedule",
		Metadata:   map[string]interface{}{"name": "dlh-pod-kill-x"},
		Spec:       map[string]interface{}{},
	}
	ref, err := c.Create(context.Background(), "run-1", res)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ref.Name != "dlh-pod-kill-x" {
		t.Errorf("ref name: %q", ref.Name)
	}
	if err := c.Delete(context.Background(), ref); err != nil {
		t.Errorf("Delete: %v", err)
	}
	// Delete-again should be tolerated (NotFound is not an error).
	if err := c.Delete(context.Background(), ref); err != nil {
		t.Errorf("Delete-again: %v", err)
	}
}

func TestLocalChaosClient_DeleteByRun(t *testing.T) {
	c := newDynFake()
	for _, n := range []string{"sched-a", "sched-b"} {
		_, err := c.Create(context.Background(), "run-1", Resource{
			APIVersion: "chaos-mesh.org/v1alpha1",
			Kind:       "Schedule",
			Metadata:   map[string]interface{}{"name": n},
			Spec:       map[string]interface{}{},
		})
		if err != nil {
			t.Fatalf("Create %s: %v", n, err)
		}
	}
	refs, err := c.ListByRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 2 {
		t.Errorf("expected 2 refs, got %d", len(refs))
	}
	if err := c.DeleteByRun(context.Background(), "run-1"); err != nil {
		t.Errorf("DeleteByRun: %v", err)
	}
	refs2, _ := c.ListByRun(context.Background(), "run-1")
	if len(refs2) != 0 {
		t.Errorf("expected 0 after DeleteByRun, got %d", len(refs2))
	}
}

func TestLocalChaosClient_ListManaged(t *testing.T) {
	c := newDynFake()
	for _, n := range []string{"a", "b"} {
		_, err := c.Create(context.Background(), "run-managed", Resource{
			APIVersion: "chaos-mesh.org/v1alpha1",
			Kind:       "Schedule",
			Metadata:   map[string]interface{}{"name": "sched-" + n},
			Spec:       map[string]interface{}{},
		})
		if err != nil {
			t.Fatalf("Create %s: %v", n, err)
		}
	}
	all, err := c.ListManaged(context.Background())
	if err != nil {
		t.Fatalf("ListManaged: %v", err)
	}
	if len(all["run-managed"]) != 2 {
		t.Errorf("expected 2 managed for run-managed, got %d (all: %+v)", len(all["run-managed"]), all)
	}
}

func TestRefEncodeDecode(t *testing.T) {
	r := Ref{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "schedules", Namespace: "dlh-test-fw", Name: "dlh-pod-kill-x"}
	got, err := DecodeRef(r.Encode())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != r {
		t.Errorf("roundtrip: %+v vs %+v", got, r)
	}
}
