package k8s

import (
	"errors"
	"reflect"
	"sort"
	"sync"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	wfinformers "github.com/argoproj/argo-workflows/v3/pkg/client/informers/externalversions"
	wflisters "github.com/argoproj/argo-workflows/v3/pkg/client/listers/workflow/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// WorkflowLister abstracts the read operations the API handlers need.
type WorkflowLister interface {
	List(filter WorkflowFilter) ([]*wfv1.Workflow, error)
	Get(name string) (*wfv1.Workflow, error)
	Subscribe() (<-chan WorkflowEvent, func())
}

// WorkflowFilter narrows a list query.
type WorkflowFilter struct {
	Scenario string
	Status   string
	Since    *time.Time
	Limit    int
}

// WorkflowEvent is emitted by the informer for SSE consumers.
type WorkflowEvent struct {
	Type     string // ADDED / MODIFIED / DELETED
	Workflow *wfv1.Workflow
}

type workflowLister struct {
	informerFactory wfinformers.SharedInformerFactory
	lister          wflisters.WorkflowLister
	namespace       string

	mu          sync.Mutex
	subscribers map[chan WorkflowEvent]struct{}
}

// NewWorkflowLister starts a SharedInformerFactory + Workflow informer
// for the namespace. The returned lister is safe for concurrent use.
// stopCh terminates the informer when closed.
func NewWorkflowLister(c *Clients, namespace string, stopCh <-chan struct{}) (WorkflowLister, error) {
	factory := wfinformers.NewSharedInformerFactoryWithOptions(c.Argo, 30*time.Second,
		wfinformers.WithNamespace(namespace))
	informer := factory.Argoproj().V1alpha1().Workflows()
	wl := &workflowLister{
		informerFactory: factory,
		lister:          informer.Lister(),
		namespace:       namespace,
		subscribers:     map[chan WorkflowEvent]struct{}{},
	}
	if _, err := informer.Informer().AddEventHandler(wl.eventHandlerFuncs()); err != nil {
		return nil, err
	}
	factory.Start(stopCh)
	synced := factory.WaitForCacheSync(stopCh)
	if !cacheSyncedAll(synced) {
		return nil, errors.New("informer cache did not sync")
	}
	return wl, nil
}

// cacheSyncedAll checks that all informers in the sync map reported true.
// WaitForCacheSync returns map[reflect.Type]bool in the argo informer factory.
func cacheSyncedAll(m map[reflect.Type]bool) bool {
	if m == nil {
		return false
	}
	for _, ok := range m {
		if !ok {
			return false
		}
	}
	return true
}

func (w *workflowLister) eventHandlerFuncs() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if wf, ok := obj.(*wfv1.Workflow); ok {
				w.broadcast(WorkflowEvent{Type: "ADDED", Workflow: wf})
			}
		},
		UpdateFunc: func(_, newObj interface{}) {
			if wf, ok := newObj.(*wfv1.Workflow); ok {
				w.broadcast(WorkflowEvent{Type: "MODIFIED", Workflow: wf})
			}
		},
		DeleteFunc: func(obj interface{}) {
			if wf, ok := obj.(*wfv1.Workflow); ok {
				w.broadcast(WorkflowEvent{Type: "DELETED", Workflow: wf})
			}
		},
	}
}

func (w *workflowLister) List(f WorkflowFilter) ([]*wfv1.Workflow, error) {
	all, err := w.lister.Workflows(w.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	return filterWorkflows(all, f), nil
}

func (w *workflowLister) Get(name string) (*wfv1.Workflow, error) {
	return w.lister.Workflows(w.namespace).Get(name)
}

func (w *workflowLister) Subscribe() (<-chan WorkflowEvent, func()) {
	ch := make(chan WorkflowEvent, 16)
	w.mu.Lock()
	w.subscribers[ch] = struct{}{}
	w.mu.Unlock()
	cancel := func() {
		w.mu.Lock()
		if _, ok := w.subscribers[ch]; ok {
			delete(w.subscribers, ch)
			close(ch)
		}
		w.mu.Unlock()
	}
	return ch, cancel
}

func (w *workflowLister) broadcast(ev WorkflowEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for ch := range w.subscribers {
		select {
		case ch <- ev:
		default:
			// Subscriber too slow — drop. Better than blocking the informer.
		}
	}
}

// filterWorkflows runs the WorkflowFilter against an in-memory slice.
// Exposed at package scope so it can be unit-tested without a running
// informer.
func filterWorkflows(items []*wfv1.Workflow, f WorkflowFilter) []*wfv1.Workflow {
	filtered := []*wfv1.Workflow{}
	for _, wf := range items {
		if f.Scenario != "" && wf.Labels["dlh.scenario"] != f.Scenario && templateRef(wf) != f.Scenario {
			continue
		}
		if f.Status != "" && string(wf.Status.Phase) != f.Status {
			continue
		}
		if f.Since != nil && wf.CreationTimestamp.Time.Before(*f.Since) {
			continue
		}
		filtered = append(filtered, wf)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreationTimestamp.After(filtered[j].CreationTimestamp.Time)
	})
	if f.Limit > 0 && len(filtered) > f.Limit {
		filtered = filtered[:f.Limit]
	}
	return filtered
}

func templateRef(wf *wfv1.Workflow) string {
	if wf.Spec.WorkflowTemplateRef != nil {
		return wf.Spec.WorkflowTemplateRef.Name
	}
	return ""
}
