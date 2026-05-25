package api

import (
	"context"
	"errors"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/auth"
	"github.com/dlh/dlh-test-fw/controlplane/internal/k8s"
	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
	mio "github.com/dlh/dlh-test-fw/controlplane/internal/minio"
	"github.com/dlh/dlh-test-fw/controlplane/internal/model"
	"github.com/dlh/dlh-test-fw/controlplane/internal/runs"
	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
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
func (h *Handlers) ListRuns(ctx context.Context, req gen.ListRunsRequestObject) (gen.ListRunsResponseObject, error) {
	f := k8s.WorkflowFilter{}
	if req.Params.Scenario != nil {
		f.Scenario = *req.Params.Scenario
	}
	if req.Params.Target != nil {
		f.Target = *req.Params.Target
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
		r := model.RunFromWorkflow(wf)
		if h.deps.Verdicts != nil {
			terminal := r.Status == gen.RunStatusSucceeded ||
				r.Status == gen.RunStatusFailed ||
				r.Status == gen.RunStatusError
			if s, ok := h.deps.Verdicts.Score(ctx, wf.Name, terminal); ok {
				r.Score = &s
			}
		}
		items = append(items, r)
	}
	return gen.ListRuns200JSONResponse{Items: items}, nil
}

// GetRun — GET /api/runs/{id}
func (h *Handlers) GetRun(ctx context.Context, req gen.GetRunRequestObject) (gen.GetRunResponseObject, error) {
	wf, err := h.deps.Workflows.Get(req.Id)
	if err != nil {
		// Workflow CR not found — fall back to MinIO manifest (TTL-collected case).
		if h.deps.Manifests != nil {
			if m, mErr := h.deps.Manifests.Read(ctx, req.Id); mErr == nil && m != nil {
				d := runDetailFromManifest(*m)
				h.addLinks(&d)
				return gen.GetRun200JSONResponse(d), nil
			}
		}
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
	h.addLinks(&detail)
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

// CreateRun — POST /api/runs
func (h *Handlers) CreateRun(ctx context.Context, req gen.CreateRunRequestObject) (gen.CreateRunResponseObject, error) {
	id, _ := auth.IdentityFromContext(ctx)
	createdBy := ""
	if id != nil {
		createdBy = id.Subject
	}
	body := req.Body
	if body == nil || body.ScenarioId == "" {
		return gen.CreateRun400Response{}, nil
	}
	params := map[string]string{}
	if body.Parameters != nil {
		for k, v := range *body.Parameters {
			params[k] = v
		}
	}
	targetID := ""
	if body.TargetId != nil {
		targetID = *body.TargetId
	}
	sr, err := h.deps.Submitter.Submit(ctx, runs.SubmitRequest{
		ScenarioID: body.ScenarioId,
		TargetID:   targetID,
		Priority:   body.Priority,
		Parameters: params,
		CreatedBy:  createdBy,
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return gen.CreateRun404Response{}, nil
		}
		return nil, err
	}
	m := runs.Manifest{
		RunID:        sr.RunID,
		Scenario:     body.ScenarioId,
		Target:       targetID,
		WorkflowName: sr.RunID,
		Parameters:   params,
		CreatedBy:    createdBy,
		Status:       "Submitted",
		StartedAt:    sr.StartedAt,
	}
	_ = h.deps.Manifests.Write(ctx, m) // best-effort; informer will write a manifest later anyway
	resp := gen.Run{
		Id:           sr.RunID,
		Scenario:     body.ScenarioId,
		Status:       gen.RunStatus("Submitted"),
		StartedAt:    sr.StartedAt,
		WorkflowName: stringPtr(sr.RunID),
	}
	if targetID != "" {
		resp.Target = &targetID
	}
	return gen.CreateRun202JSONResponse(resp), nil
}

func stringPtr(s string) *string { return &s }

// CancelRun — DELETE /api/runs/{id}
func (h *Handlers) CancelRun(ctx context.Context, req gen.CancelRunRequestObject) (gen.CancelRunResponseObject, error) {
	if _, err := h.deps.Workflows.Get(req.Id); err != nil {
		return gen.CancelRun404Response{}, nil
	}
	// Best-effort chaos cleanup first.
	if h.deps.Chaos != nil {
		_ = h.deps.Chaos.DeleteByRun(ctx, req.Id)
	}
	// Argo's "shutdown=Terminate" annotation patch is the official cancel path.
	patch := []byte(`{"spec":{"shutdown":"Terminate"}}`)
	_, err := h.deps.ArgoClient.ArgoprojV1alpha1().Workflows(h.deps.Submitter.Namespace).Patch(
		ctx, req.Id, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return nil, err
	}
	return gen.CancelRun202Response{}, nil
}
// Unreachable: /internal/chaos is mounted directly on chi outside the
// strict-server chain. This stub satisfies the strict interface.
func (h *Handlers) CreateChaos(_ context.Context, _ gen.CreateChaosRequestObject) (gen.CreateChaosResponseObject, error) {
	return gen.CreateChaos500Response{}, nil
}

// Unreachable: see CreateChaos.
func (h *Handlers) DeleteChaos(_ context.Context, _ gen.DeleteChaosRequestObject) (gen.DeleteChaosResponseObject, error) {
	return gen.DeleteChaos401Response{}, nil
}

// Phase D — Task 9: real targets handlers backed by the registry.

func (h *Handlers) ListTargets(_ context.Context, _ gen.ListTargetsRequestObject) (gen.ListTargetsResponseObject, error) {
	if h.deps.Targets == nil {
		return gen.ListTargets200JSONResponse{Items: []gen.Target{}}, nil
	}
	loaded := h.deps.Targets.List()
	items := make([]gen.Target, 0, len(loaded))
	for _, t := range loaded {
		items = append(items, targetDTO(t))
	}
	return gen.ListTargets200JSONResponse{Items: items}, nil
}

func (h *Handlers) GetTarget(_ context.Context, req gen.GetTargetRequestObject) (gen.GetTargetResponseObject, error) {
	if h.deps.Targets == nil {
		return gen.GetTarget404Response{}, nil
	}
	t := h.deps.Targets.Get(req.Id)
	if t == nil {
		return gen.GetTarget404Response{}, nil
	}
	return gen.GetTarget200JSONResponse(targetDTO(t)), nil
}

func (h *Handlers) TestTargetConnection(ctx context.Context, req gen.TestTargetConnectionRequestObject) (gen.TestTargetConnectionResponseObject, error) {
	if h.deps.Targets == nil {
		return gen.TestTargetConnection404Response{}, nil
	}
	t := h.deps.Targets.Get(req.Id)
	if t == nil {
		return gen.TestTargetConnection404Response{}, nil
	}
	res := targets.Probe(ctx, t)
	latencyNanos := res.Latency.Nanoseconds()
	errStr := res.Error
	return gen.TestTargetConnection200JSONResponse{
		Ok:           res.OK,
		LatencyNanos: &latencyNanos,
		Error:        &errStr,
	}, nil
}

// grafanaEntry aliases the anonymous element type of gen.RunDetail.GrafanaUrls
// so we can build the slice readably.
type grafanaEntry = struct {
	Label string `json:"label"`
	Url   string `json:"url"`
}

// addLinks enriches a RunDetail with Argo/Grafana deep links from configured
// base URLs. No-op for any link whose base URL is unset.
//
// The Argo link is added for every run (every run is a Workflow). Grafana links
// are added only for runs that produced SLO/load metrics — i.e. a verdict or a
// score is present. Bare chaos-only runs (e.g. chaos-kafka-broker-partition) run
// no k6 load and have no verdict, so their dashboards would be empty; we omit the
// buttons rather than link to "No data".
func (h *Handlers) addLinks(d *gen.RunDetail) {
	lc := h.deps.Links
	wfName := ""
	if d.WorkflowName != nil {
		wfName = *d.WorkflowName
	}
	if u := links.ArgoURL(lc.ArgoBaseURL, lc.Namespace, wfName); u != "" {
		d.ArgoUrl = &u
	}
	hasMetrics := d.Verdict != nil || d.Score != nil
	if !hasMetrics {
		return
	}
	if urls := links.GrafanaURLs(lc.GrafanaBaseURL, d.Scenario, wfName, d.StartedAt, d.FinishedAt); len(urls) > 0 {
		arr := make([]grafanaEntry, 0, len(urls))
		for _, u := range urls {
			arr = append(arr, grafanaEntry{Label: u.Label, Url: u.URL})
		}
		d.GrafanaUrls = &arr
	}
}

// runDetailFromManifest builds a RunDetail from a stored MinIO manifest.
// Used as fallback when the Workflow CR has been TTL-collected by Argo.
func runDetailFromManifest(m runs.Manifest) gen.RunDetail {
	d := gen.RunDetail{
		Id:           m.RunID,
		Scenario:     m.Scenario,
		Status:       gen.RunDetailStatus(m.Status),
		StartedAt:    m.StartedAt,
		WorkflowName: stringPtr(m.WorkflowName),
	}
	if m.FinishedAt != nil {
		d.FinishedAt = m.FinishedAt
	}
	if m.Score != nil {
		d.Score = m.Score
	}
	return d
}

func (h *Handlers) OidcExchange(ctx context.Context, req gen.OidcExchangeRequestObject) (gen.OidcExchangeResponseObject, error) {
	return h.handleOidcExchange(ctx, req.Body)
}
func (h *Handlers) GetAuthInfo(_ context.Context, _ gen.GetAuthInfoRequestObject) (gen.GetAuthInfoResponseObject, error) {
	return h.handleAuthInfo(), nil
}

func (h *Handlers) ListSchedules(ctx context.Context, _ gen.ListSchedulesRequestObject) (gen.ListSchedulesResponseObject, error) {
	return h.handleListSchedules(ctx)
}
func (h *Handlers) CreateSchedule(ctx context.Context, req gen.CreateScheduleRequestObject) (gen.CreateScheduleResponseObject, error) {
	return h.handleCreateSchedule(ctx, req)
}
func (h *Handlers) GetSchedule(ctx context.Context, req gen.GetScheduleRequestObject) (gen.GetScheduleResponseObject, error) {
	return h.handleGetSchedule(ctx, req)
}
func (h *Handlers) DeleteSchedule(ctx context.Context, req gen.DeleteScheduleRequestObject) (gen.DeleteScheduleResponseObject, error) {
	return h.handleDeleteSchedule(ctx, req)
}
func (h *Handlers) PauseSchedule(ctx context.Context, req gen.PauseScheduleRequestObject) (gen.PauseScheduleResponseObject, error) {
	return h.handlePauseSchedule(ctx, req)
}
func (h *Handlers) ResumeSchedule(ctx context.Context, req gen.ResumeScheduleRequestObject) (gen.ResumeScheduleResponseObject, error) {
	return h.handleResumeSchedule(ctx, req)
}
