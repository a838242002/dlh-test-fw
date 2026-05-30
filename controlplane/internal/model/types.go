package model

import (
	"sort"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
)

// IsScenarioTemplate reports whether a WorkflowTemplate is a runnable scenario
// (vs a chaos/fixture/util building block), per its dlh.category label.
func IsScenarioTemplate(t *wfv1.WorkflowTemplate) bool {
	return t.Labels["dlh.category"] == "scenario"
}

// ScenarioFromTemplate maps a WorkflowTemplate to the OpenAPI Scenario DTO.
//
// Scenario.Parameters is an inline anonymous struct slice in the generated
// code — there is no named gen.ScenarioParameters type.
func ScenarioFromTemplate(t *wfv1.WorkflowTemplate) gen.Scenario {
	s := gen.Scenario{
		Id:          t.Name,
		DisplayName: t.Name,
	}

	// Derive target type from scenario id via the links package (mirrors web-side logic).
	// DeriveTargetType returns "generic" when nothing matches; we normalise that to ""
	// so ScenarioDescription falls through to its id-only branch.
	targetType := links.DeriveTargetType(t.Name)
	if targetType == "generic" {
		targetType = ""
	}
	if targetType != "" {
		tt := targetType
		s.TargetType = &tt
	} else if v := t.Annotations["dlh.target-type"]; v != "" {
		tt := v
		s.TargetType = &tt
		targetType = v
	}

	// Extract slo_name from the WorkflowTemplate's top-level arguments so the
	// description can name the SLO being evaluated.
	var sloName string
	for _, p := range t.Spec.Arguments.Parameters {
		if p.Name == "slo_name" {
			if p.Value != nil {
				sloName = p.Value.String()
			} else if p.Default != nil {
				sloName = p.Default.String()
			}
			break
		}
	}

	// Build description: dlh.scenario/description annotation wins; else derive.
	desc := ScenarioDescription(t.Annotations, t.Name, targetType, sloName)
	s.Description = &desc

	if len(t.Spec.Arguments.Parameters) > 0 {
		params := make([]struct {
			Default     *string `json:"default,omitempty"`
			Description *string `json:"description,omitempty"`
			Name        string  `json:"name"`
		}, 0, len(t.Spec.Arguments.Parameters))
		for _, p := range t.Spec.Arguments.Parameters {
			entry := struct {
				Default     *string `json:"default,omitempty"`
				Description *string `json:"description,omitempty"`
				Name        string  `json:"name"`
			}{Name: p.Name}
			if p.Default != nil {
				d := p.Default.String()
				entry.Default = &d
			}
			if p.Description != nil {
				ds := p.Description.String()
				entry.Description = &ds
			}
			params = append(params, entry)
		}
		s.Parameters = &params
	}
	return s
}

// RunFromWorkflow maps a Workflow CR to the OpenAPI Run summary DTO.
func RunFromWorkflow(wf *wfv1.Workflow) gen.Run {
	r := gen.Run{
		Id:        wf.Name,
		StartedAt: wf.CreationTimestamp.Time,
		Status:    gen.RunStatus(mapPhase(string(wf.Status.Phase))),
	}
	if wf.Spec.WorkflowTemplateRef != nil {
		r.Scenario = wf.Spec.WorkflowTemplateRef.Name
	} else if v := wf.Labels["dlh.scenario"]; v != "" {
		r.Scenario = v
	}
	if !wf.Status.FinishedAt.IsZero() {
		t := wf.Status.FinishedAt.Time
		r.FinishedAt = &t
	}
	if v := wf.Labels["dlh.target"]; v != "" {
		r.Target = &v
	}
	if wf.Spec.Priority != nil {
		p := int(*wf.Spec.Priority)
		r.Priority = &p
	}
	for _, owner := range wf.OwnerReferences {
		if owner.Kind == "CronWorkflow" {
			kind := "Schedule"
			idVal := owner.Name
			r.TriggeredBy = &struct {
				Id   *string `json:"id,omitempty"`
				Kind *string `json:"kind,omitempty"`
			}{Id: &idVal, Kind: &kind}
			break
		}
	}
	name := wf.Name
	r.WorkflowName = &name
	return r
}

// RunDetailFromWorkflow maps a Workflow CR to the flat RunDetail DTO.
// RunDetail does NOT embed Run — it repeats the same fields at the top level.
func RunDetailFromWorkflow(wf *wfv1.Workflow) gen.RunDetail {
	d := gen.RunDetail{
		Id:        wf.Name,
		StartedAt: wf.CreationTimestamp.Time,
		Status:    gen.RunDetailStatus(mapPhase(string(wf.Status.Phase))),
	}
	if wf.Spec.WorkflowTemplateRef != nil {
		d.Scenario = wf.Spec.WorkflowTemplateRef.Name
	} else if v := wf.Labels["dlh.scenario"]; v != "" {
		d.Scenario = v
	}
	if !wf.Status.FinishedAt.IsZero() {
		t := wf.Status.FinishedAt.Time
		d.FinishedAt = &t
	}
	if v := wf.Labels["dlh.target"]; v != "" {
		d.Target = &v
	}
	for _, owner := range wf.OwnerReferences {
		if owner.Kind == "CronWorkflow" {
			kind := "Schedule"
			idVal := owner.Name
			d.TriggeredBy = &struct {
				Id   *string `json:"id,omitempty"`
				Kind *string `json:"kind,omitempty"`
			}{Id: &idVal, Kind: &kind}
			break
		}
	}
	name := wf.Name
	d.WorkflowName = &name

	// Populate description: prefer dlh.scenario/description annotation, else derived.
	desc := ScenarioDescription(wf.Annotations, d.Scenario, links.DeriveTargetType(d.Scenario), "")
	d.Description = &desc

	// Populate priority from workflow spec (display-only).
	if wf.Spec.Priority != nil {
		p := int(*wf.Spec.Priority)
		d.Priority = &p
	}

	if len(wf.Status.Nodes) > 0 {
		// Steps uses an inline anonymous struct type in the generated code.
		steps := make([]struct {
			FinishedAt *time.Time `json:"finishedAt,omitempty"`
			Message    *string    `json:"message,omitempty"`
			Name       string     `json:"name"`
			Phase      string     `json:"phase"`
			StartedAt  *time.Time `json:"startedAt,omitempty"`
		}, 0, len(wf.Status.Nodes))
		for _, n := range wf.Status.Nodes {
			step := struct {
				FinishedAt *time.Time `json:"finishedAt,omitempty"`
				Message    *string    `json:"message,omitempty"`
				Name       string     `json:"name"`
				Phase      string     `json:"phase"`
				StartedAt  *time.Time `json:"startedAt,omitempty"`
			}{
				Name:  n.DisplayName,
				Phase: string(n.Phase),
			}
			if !n.StartedAt.IsZero() {
				t := n.StartedAt.Time
				step.StartedAt = &t
			}
			if !n.FinishedAt.IsZero() {
				t := n.FinishedAt.Time
				step.FinishedAt = &t
			}
			if n.Message != "" {
				m := n.Message
				step.Message = &m
			}
			steps = append(steps, step)
		}
		sort.SliceStable(steps, func(i, j int) bool {
			si, sj := steps[i].StartedAt, steps[j].StartedAt
			switch {
			case si == nil && sj == nil:
				return steps[i].Name < steps[j].Name
			case si == nil:
				return false
			case sj == nil:
				return true
			case si.Equal(*sj):
				return steps[i].Name < steps[j].Name
			default:
				return si.Before(*sj)
			}
		})
		d.Steps = &steps
	}
	return d
}

func mapPhase(phase string) string {
	switch phase {
	case "":
		return "Pending"
	case "Pending", "Running", "Succeeded", "Failed", "Error":
		return phase
	default:
		return "Unknown"
	}
}
