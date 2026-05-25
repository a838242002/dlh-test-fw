// Package queue builds the per-target-type semaphore view consumed by
// GET /api/queue. It is pure: given the current workflows + the lock keys,
// it groups Running holders and orders Pending runs the way Argo releases
// them (priority desc, then oldest first).
package queue

import (
	"sort"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

	"github.com/dlh/dlh-test-fw/controlplane/internal/links"
)

// LockKey is one semaphore key + its slot count (from dlh-scenario-locks).
type LockKey struct {
	Key   string
	Slots int
}

// Entry is one workflow in a lane.
type Entry struct {
	ID          string
	Scenario    string
	Priority    *int
	SubmittedAt time.Time
}

// Lane is the running holder(s) + ordered pending queue for one semaphore key.
type Lane struct {
	Key     string
	Slots   int
	Running []Entry
	Pending []Entry
}

func isTerminal(p wfv1.WorkflowPhase) bool {
	return p == wfv1.WorkflowSucceeded || p == wfv1.WorkflowFailed || p == wfv1.WorkflowError
}

func entryOf(w *wfv1.Workflow) Entry {
	e := Entry{
		ID:          w.Name,
		SubmittedAt: w.CreationTimestamp.Time,
	}
	if w.Spec.WorkflowTemplateRef != nil {
		e.Scenario = w.Spec.WorkflowTemplateRef.Name
	} else if v := w.Labels["dlh.scenario"]; v != "" {
		e.Scenario = v
	}
	if w.Spec.Priority != nil {
		p := int(*w.Spec.Priority)
		e.Priority = &p
	}
	return e
}

// prioVal returns the comparable priority (nil sorts as 0, matching Argo default).
func prioVal(e Entry) int {
	if e.Priority == nil {
		return 0
	}
	return *e.Priority
}

// BuildLanes groups workflows by derived target type into one lane per lock key,
// preserving the key order given. Running workflows are holders; Pending ones
// are ordered priority-desc then oldest-first.
func BuildLanes(wfs []*wfv1.Workflow, keys []LockKey) []Lane {
	running := map[string][]Entry{}
	pending := map[string][]Entry{}
	for _, w := range wfs {
		if w == nil || isTerminal(w.Status.Phase) {
			continue
		}
		scenario := ""
		if w.Spec.WorkflowTemplateRef != nil {
			scenario = w.Spec.WorkflowTemplateRef.Name
		} else {
			scenario = w.Labels["dlh.scenario"]
		}
		key := links.DeriveTargetType(scenario)
		e := entryOf(w)
		switch w.Status.Phase {
		case wfv1.WorkflowRunning:
			running[key] = append(running[key], e)
		case wfv1.WorkflowPending, "":
			pending[key] = append(pending[key], e)
		default:
			// Unknown / non-running, non-pending, non-terminal — treat as pending.
			pending[key] = append(pending[key], e)
		}
	}

	lanes := make([]Lane, 0, len(keys))
	for _, k := range keys {
		lane := Lane{Key: k.Key, Slots: k.Slots, Running: running[k.Key], Pending: pending[k.Key]}
		sort.SliceStable(lane.Running, func(i, j int) bool {
			return lane.Running[i].SubmittedAt.Before(lane.Running[j].SubmittedAt)
		})
		sort.SliceStable(lane.Pending, func(i, j int) bool {
			pi, pj := prioVal(lane.Pending[i]), prioVal(lane.Pending[j])
			if pi != pj {
				return pi > pj // higher priority first
			}
			return lane.Pending[i].SubmittedAt.Before(lane.Pending[j].SubmittedAt) // oldest first
		})
		lanes = append(lanes, lane)
	}
	return lanes
}
