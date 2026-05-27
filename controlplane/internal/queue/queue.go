// Package queue builds the per-target-type semaphore view consumed by
// GET /api/queue. It is pure: given the current workflows + the lock keys,
// it groups Running holders and orders Pending runs the way Argo releases
// them (priority desc, then oldest first).
package queue

import (
	"sort"
	"strings"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
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

// lockKey extracts the bare semaphore key (last path segment) from Argo's
// fully-qualified semaphore name, e.g.
// "dlh-test-fw/ConfigMap/dlh-scenario-locks/mysql" -> "mysql".
func lockKey(semaphore string) string {
	if i := strings.LastIndex(semaphore, "/"); i >= 0 {
		return semaphore[i+1:]
	}
	return semaphore
}

// addEntry appends e under key, deduping by ID. Argo's sync manager places a
// given lock in exactly one of Holding or Waiting per workflow, so cross-map
// duplicates cannot occur in practice — this guard is defensive against
// duplicate entries within a single status object.
func addEntry(m map[string][]Entry, key string, e Entry) {
	for _, x := range m[key] {
		if x.ID == e.ID {
			return
		}
	}
	m[key] = append(m[key], e)
}

// BuildLanes groups non-terminal workflows into one lane per lock key,
// preserving the key order given. Classification comes from Argo's own
// synchronization record, NOT the workflow phase: a workflow is a Running
// holder of a lane iff its status lists that lane's lock in .Holding, and a
// Queued waiter iff in .Waiting. Workflows contending for nothing (pre-gate or
// post-release) appear in no lane. Pending entries are ordered priority-desc
// then oldest-first (Argo's release order).
func BuildLanes(wfs []*wfv1.Workflow, keys []LockKey) []Lane {
	known := make(map[string]bool, len(keys))
	for _, k := range keys {
		known[k.Key] = true
	}

	running := map[string][]Entry{}
	pending := map[string][]Entry{}
	for _, w := range wfs {
		if w == nil || isTerminal(w.Status.Phase) {
			continue
		}
		sync := w.Status.Synchronization
		if sync == nil || sync.Semaphore == nil {
			continue
		}
		e := entryOf(w)
		for _, h := range sync.Semaphore.Holding {
			if key := lockKey(h.Semaphore); known[key] {
				addEntry(running, key, e)
			}
		}
		for _, h := range sync.Semaphore.Waiting {
			if key := lockKey(h.Semaphore); known[key] {
				addEntry(pending, key, e)
			}
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
