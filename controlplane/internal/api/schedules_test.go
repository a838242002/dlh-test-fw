package api

import (
	"context"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfake "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/schedules"
)

func TestCreateSchedule_HappyPath(t *testing.T) {
	tmpl := &wfv1.WorkflowTemplate{ObjectMeta: metav1.ObjectMeta{Name: "mysql-pod-delete", Namespace: "dlh-test-fw"}}
	argo := wfake.NewSimpleClientset(tmpl)
	deps := &Deps{Schedules: &schedules.Manager{Argo: argo, Namespace: "dlh-test-fw"}}
	h := &Handlers{deps: deps}
	resp, err := h.CreateSchedule(context.Background(), gen.CreateScheduleRequestObject{
		Body: &gen.CreateScheduleRequest{
			Id:         "nightly",
			ScenarioId: "mysql-pod-delete",
			Cron:       "0 2 * * *",
		},
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	out, ok := resp.(gen.CreateSchedule201JSONResponse)
	if !ok {
		t.Fatalf("response type: %T", resp)
	}
	if out.Id != "nightly" {
		t.Errorf("id: %q", out.Id)
	}
}

func TestCreateSchedule_404OnUnknownScenario(t *testing.T) {
	argo := wfake.NewSimpleClientset()
	deps := &Deps{Schedules: &schedules.Manager{Argo: argo, Namespace: "dlh-test-fw"}}
	h := &Handlers{deps: deps}
	resp, err := h.CreateSchedule(context.Background(), gen.CreateScheduleRequestObject{
		Body: &gen.CreateScheduleRequest{Id: "x", ScenarioId: "nope", Cron: "0 * * * *"},
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if _, ok := resp.(gen.CreateSchedule404Response); !ok {
		t.Errorf("expected 404, got %T", resp)
	}
}

func TestPauseSchedule_404OnUnknown(t *testing.T) {
	deps := &Deps{Schedules: &schedules.Manager{Argo: wfake.NewSimpleClientset(), Namespace: "dlh-test-fw"}}
	h := &Handlers{deps: deps}
	resp, _ := h.PauseSchedule(context.Background(), gen.PauseScheduleRequestObject{Id: "nope"})
	if _, ok := resp.(gen.PauseSchedule404Response); !ok {
		t.Errorf("expected 404, got %T", resp)
	}
}
