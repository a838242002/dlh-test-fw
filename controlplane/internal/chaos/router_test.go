package chaos

import (
	"context"
	"testing"

	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
)

func TestRouter_PickLocalOnEmptyTargetID(t *testing.T) {
	local := newDynFake() // from local_test.go
	r := &Router{Local: local}
	c, err := r.pick("")
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if c != local {
		t.Errorf("expected local client, got %T", c)
	}
}

func TestRouter_PickUnknownTarget(t *testing.T) {
	r := &Router{Local: newDynFake(), Registry: targets.NewRegistry()}
	_, err := r.pick("nope")
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
}

func TestRouter_CreateForTarget_EmptyRoutesLocal(t *testing.T) {
	local := newDynFake()
	r := &Router{Local: local}
	ref, err := r.CreateForTarget(context.Background(), "run-1", "", Resource{
		APIVersion: "chaos-mesh.org/v1alpha1",
		Kind:       "Schedule",
		Metadata:   map[string]interface{}{"name": "sched-x"},
		Spec:       map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("CreateForTarget: %v", err)
	}
	if ref.Name != "sched-x" {
		t.Errorf("ref name: %q", ref.Name)
	}
	// Confirm the chaos landed on the local client.
	refs, _ := local.ListByRun(context.Background(), "run-1")
	if len(refs) != 1 {
		t.Errorf("expected 1 local ref, got %d", len(refs))
	}
}
