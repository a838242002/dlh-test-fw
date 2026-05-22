package api

import (
	"context"
	"testing"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
)

func TestListTargets_EmptyRegistry(t *testing.T) {
	deps := &Deps{Targets: targets.NewRegistry()}
	h := &Handlers{deps: deps}
	resp, err := h.ListTargets(context.Background(), gen.ListTargetsRequestObject{})
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	out, ok := resp.(gen.ListTargets200JSONResponse)
	if !ok {
		t.Fatalf("response type: %T", resp)
	}
	if len(out.Items) != 0 {
		t.Errorf("expected empty, got %d", len(out.Items))
	}
}

func TestGetTarget_404OnUnknown(t *testing.T) {
	deps := &Deps{Targets: targets.NewRegistry()}
	h := &Handlers{deps: deps}
	resp, err := h.GetTarget(context.Background(), gen.GetTargetRequestObject{Id: "nope"})
	if err != nil {
		t.Fatalf("GetTarget: %v", err)
	}
	if _, ok := resp.(gen.GetTarget404Response); !ok {
		t.Fatalf("expected 404, got %T", resp)
	}
}

func TestListTargets_PopulatedRegistry(t *testing.T) {
	r := targets.NewRegistry()
	r.Replace(map[string]*targets.LoadedTarget{
		"staging-mysql": {Target: targets.Target{
			ID:                 "staging-mysql",
			DisplayName:        "Staging MySQL",
			AllowedTargetTypes: []string{"mysql"},
			KubeconfigSecret:   "dlh-target-staging-mysql",
		}},
	})
	deps := &Deps{Targets: r}
	h := &Handlers{deps: deps}
	resp, _ := h.ListTargets(context.Background(), gen.ListTargetsRequestObject{})
	out := resp.(gen.ListTargets200JSONResponse)
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 target, got %d", len(out.Items))
	}
	if out.Items[0].Id != "staging-mysql" {
		t.Errorf("id: %q", out.Items[0].Id)
	}
}
