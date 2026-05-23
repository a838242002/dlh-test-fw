package api

import (
	"context"
	"errors"
	"strings"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/auth"
	"github.com/dlh/dlh-test-fw/controlplane/internal/schedules"
)

// scheduleDTO maps an Argo CronWorkflow to the OpenAPI Schedule.
// Optional fields use *T pointers per codegen output.
func scheduleDTO(c *wfv1.CronWorkflow) gen.Schedule {
	scenario := ""
	if c.Spec.WorkflowSpec.WorkflowTemplateRef != nil {
		scenario = c.Spec.WorkflowSpec.WorkflowTemplateRef.Name
	} else if v := c.Labels["dlh.scenario"]; v != "" {
		scenario = v
	}
	target := ""
	if c.Spec.WorkflowMetadata != nil {
		if v := c.Spec.WorkflowMetadata.Labels["dlh.target"]; v != "" {
			target = v
		}
	}
	suspended := c.Spec.Suspend
	tz := c.Spec.Timezone
	createdBy := ""
	if v, ok := c.Annotations["dlh.created-by"]; ok {
		createdBy = v
	}
	params := map[string]string{}
	for _, p := range c.Spec.WorkflowSpec.Arguments.Parameters {
		if p.Value != nil {
			params[p.Name] = p.Value.String()
		}
	}
	dto := gen.Schedule{
		Id:        c.Name,
		Scenario:  scenario,
		Cron:      c.Spec.Schedule,
		Suspended: &suspended,
	}
	if target != "" {
		dto.Target = &target
	}
	if tz != "" {
		dto.Timezone = &tz
	}
	if createdBy != "" {
		dto.CreatedBy = &createdBy
	}
	if len(params) > 0 {
		dto.Parameters = &params
	}
	if c.Status.LastScheduledTime != nil {
		t := c.Status.LastScheduledTime.Time
		dto.LastScheduledAt = &t
	}
	active := int32(len(c.Status.Active))
	dto.ActiveCount = &active
	succ := c.Status.Succeeded
	dto.SuccessfulCount = &succ
	fail := c.Status.Failed
	dto.FailedCount = &fail
	return dto
}

func (h *Handlers) handleListSchedules(ctx context.Context) (gen.ListSchedulesResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.ListSchedules200JSONResponse{Items: []gen.Schedule{}}, nil
	}
	list, err := h.deps.Schedules.List(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]gen.Schedule, 0, len(list))
	for i := range list {
		items = append(items, scheduleDTO(&list[i]))
	}
	return gen.ListSchedules200JSONResponse{Items: items}, nil
}

func (h *Handlers) handleCreateSchedule(ctx context.Context, req gen.CreateScheduleRequestObject) (gen.CreateScheduleResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.CreateSchedule400Response{}, nil
	}
	body := req.Body
	if body == nil {
		return gen.CreateSchedule400Response{}, nil
	}
	id, _ := auth.IdentityFromContext(ctx)
	createdBy := ""
	if id != nil {
		createdBy = id.Subject
	}
	mr := schedules.CreateRequest{
		Name:       body.Id,
		ScenarioID: body.ScenarioId,
		Cron:       body.Cron,
		CreatedBy:  createdBy,
	}
	if body.TargetId != nil {
		mr.TargetID = *body.TargetId
	}
	if body.Timezone != nil {
		mr.Timezone = *body.Timezone
	}
	if body.Parameters != nil {
		mr.Parameters = *body.Parameters
	}
	got, err := h.deps.Schedules.Create(ctx, mr)
	if err != nil {
		s := err.Error()
		switch {
		case strings.Contains(s, "not found"):
			return gen.CreateSchedule404Response{}, nil
		case strings.Contains(s, "already exists"):
			return gen.CreateSchedule409Response{}, nil
		default:
			return gen.CreateSchedule400Response{}, nil
		}
	}
	return gen.CreateSchedule201JSONResponse(scheduleDTO(got)), nil
}

func (h *Handlers) handleGetSchedule(ctx context.Context, req gen.GetScheduleRequestObject) (gen.GetScheduleResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.GetSchedule404Response{}, nil
	}
	got, err := h.deps.Schedules.Get(ctx, req.Id)
	if errors.Is(err, schedules.ErrNotFound) {
		return gen.GetSchedule404Response{}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetSchedule200JSONResponse(scheduleDTO(got)), nil
}

func (h *Handlers) handleDeleteSchedule(ctx context.Context, req gen.DeleteScheduleRequestObject) (gen.DeleteScheduleResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.DeleteSchedule204Response{}, nil
	}
	if err := h.deps.Schedules.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteSchedule204Response{}, nil
}

func (h *Handlers) handlePauseSchedule(ctx context.Context, req gen.PauseScheduleRequestObject) (gen.PauseScheduleResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.PauseSchedule404Response{}, nil
	}
	if err := h.deps.Schedules.Pause(ctx, req.Id); err != nil {
		if errors.Is(err, schedules.ErrNotFound) {
			return gen.PauseSchedule404Response{}, nil
		}
		return nil, err
	}
	return gen.PauseSchedule204Response{}, nil
}

func (h *Handlers) handleResumeSchedule(ctx context.Context, req gen.ResumeScheduleRequestObject) (gen.ResumeScheduleResponseObject, error) {
	if h.deps.Schedules == nil {
		return gen.ResumeSchedule404Response{}, nil
	}
	if err := h.deps.Schedules.Resume(ctx, req.Id); err != nil {
		if errors.Is(err, schedules.ErrNotFound) {
			return gen.ResumeSchedule404Response{}, nil
		}
		return nil, err
	}
	return gen.ResumeSchedule204Response{}, nil
}
