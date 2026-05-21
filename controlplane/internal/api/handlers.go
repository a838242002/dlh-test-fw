package api

import (
	"context"
	"errors"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
	"github.com/dlh/dlh-test-fw/controlplane/internal/model"
)

// Handlers implements the oapi-codegen StrictServerInterface.
type Handlers struct {
	deps *Deps
}

// ListScenarios — GET /api/scenarios
func (h *Handlers) ListScenarios(ctx context.Context, _ gen.ListScenariosRequestObject) (gen.ListScenariosResponseObject, error) {
	tmpls, err := h.deps.Templates.ListTemplates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Scenario, 0, len(tmpls))
	for i := range tmpls {
		out = append(out, model.ScenarioFromTemplate(&tmpls[i]))
	}
	return gen.ListScenarios200JSONResponse{Items: out}, nil
}

// GetScenario — GET /api/scenarios/{id}
func (h *Handlers) GetScenario(ctx context.Context, req gen.GetScenarioRequestObject) (gen.GetScenarioResponseObject, error) {
	tmpl, err := h.deps.Templates.GetTemplate(ctx, req.Id)
	if err != nil {
		return gen.GetScenario404Response{}, nil
	}
	s := model.ScenarioFromTemplate(tmpl)
	return gen.GetScenario200JSONResponse(s), nil
}

// ListRuns — GET /api/runs
func (h *Handlers) ListRuns(_ context.Context, req gen.ListRunsRequestObject) (gen.ListRunsResponseObject, error) {
	f := k8s.WorkflowFilter{}
	if req.Params.Scenario != nil {
		f.Scenario = *req.Params.Scenario
	}
	if req.Params.Status != nil {
		f.Status = *req.Params.Status
	}
	if req.Params.Since != nil {
		t := *req.Params.Since
		f.Since = &t
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	wfs, err := h.deps.Workflows.List(f)
	if err != nil {
		return nil, err
	}
	items := make([]gen.Run, 0, len(wfs))
	for _, wf := range wfs {
		items = append(items, model.RunFromWorkflow(wf))
	}
	return gen.ListRuns200JSONResponse{Items: items}, nil
}

// GetRun — GET /api/runs/{id}
func (h *Handlers) GetRun(ctx context.Context, req gen.GetRunRequestObject) (gen.GetRunResponseObject, error) {
	wf, err := h.deps.Workflows.Get(req.Id)
	if err != nil {
		return gen.GetRun404Response{}, nil
	}
	detail := model.RunDetailFromWorkflow(wf)
	if report, err := h.deps.Reports.Read(ctx, wf.Name); err == nil {
		v := map[string]interface{}(report)
		detail.Verdict = &v
	} else if !errors.Is(err, mio.ErrReportNotFound) {
		// Non-404 error from MinIO: log but continue — run detail is still
		// useful without verdict.
		_ = err
	}
	return gen.GetRun200JSONResponse(detail), nil
}

// StreamRunEvents — stub; real SSE handler is mounted directly in Task 9.
func (h *Handlers) StreamRunEvents(_ context.Context, _ gen.StreamRunEventsRequestObject) (gen.StreamRunEventsResponseObject, error) {
	return gen.StreamRunEvents200TexteventStreamResponse{}, nil
}

// GetHealthz / GetReadyz satisfy the StrictServerInterface contract but the
// chi router answers these at the root level before they reach the strict
// handler path.
func (h *Handlers) GetHealthz(_ context.Context, _ gen.GetHealthzRequestObject) (gen.GetHealthzResponseObject, error) {
	return gen.GetHealthz200Response{}, nil
}

func (h *Handlers) GetReadyz(_ context.Context, _ gen.GetReadyzRequestObject) (gen.GetReadyzResponseObject, error) {
	return gen.GetReadyz200Response{}, nil
}

// Phase C stubs. Real implementations land in Tasks 5 (CreateRun, CancelRun)
// and Task 11 (CreateChaos, DeleteChaos — but those run on chi directly,
// not through the strict server, so these stubs are intentionally
// unreachable in production).
func (h *Handlers) CreateRun(_ context.Context, _ gen.CreateRunRequestObject) (gen.CreateRunResponseObject, error) {
	return gen.CreateRun400Response{}, nil
}
func (h *Handlers) CancelRun(_ context.Context, _ gen.CancelRunRequestObject) (gen.CancelRunResponseObject, error) {
	return gen.CancelRun404Response{}, nil
}
func (h *Handlers) CreateChaos(_ context.Context, _ gen.CreateChaosRequestObject) (gen.CreateChaosResponseObject, error) {
	return gen.CreateChaos500Response{}, nil
}
func (h *Handlers) DeleteChaos(_ context.Context, _ gen.DeleteChaosRequestObject) (gen.DeleteChaosResponseObject, error) {
	return gen.DeleteChaos401Response{}, nil
}
