// Package targets owns the registry of remote target clusters that the
// controlplane can inject chaos into. Definitions are loaded from a
// Kubernetes ConfigMap (dlh-targets) + per-target kubeconfig Secrets,
// both Argo-CD-synced. The registry refreshes every 30s by re-reading
// both resources and atomically swapping the cache.
package targets

import (
	"sync"
	"time"

	"k8s.io/client-go/rest"
)

// Target describes one remote cluster the controlplane can talk to.
type Target struct {
	// ID is the user-facing identifier (e.g. "staging-mysql"). Stable.
	ID string `yaml:"id"`
	// DisplayName is human-readable. Defaults to ID if empty.
	DisplayName string `yaml:"displayName,omitempty"`
	// KubeconfigSecret names the Secret holding the kubeconfig (key: kubeconfig).
	KubeconfigSecret string `yaml:"kubeconfigSecret"`
	// AllowedTargetTypes filters which scenarios can target this cluster.
	// Empty list = no filter (any scenario allowed).
	AllowedTargetTypes []string `yaml:"allowedTargetTypes,omitempty"`
	// Namespace is the chaos namespace on the remote cluster. Defaults to "dlh-test-fw".
	Namespace string `yaml:"namespace,omitempty"`
}

// LoadedTarget is a Target plus its parsed *rest.Config (kubeconfig).
type LoadedTarget struct {
	Target
	RestConfig *rest.Config
	LastSeen   time.Time
}

// Registry holds the current set of loaded targets, refreshed periodically.
type Registry struct {
	mu     sync.RWMutex
	loaded map[string]*LoadedTarget // id -> LoadedTarget
}

// NewRegistry returns an empty registry. Call Refresh() to populate.
func NewRegistry() *Registry {
	return &Registry{loaded: map[string]*LoadedTarget{}}
}

// Get returns the LoadedTarget for an id, or nil if not registered.
func (r *Registry) Get(id string) *LoadedTarget {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loaded[id]
}

// List returns a snapshot of all loaded targets.
func (r *Registry) List() []*LoadedTarget {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*LoadedTarget, 0, len(r.loaded))
	for _, t := range r.loaded {
		out = append(out, t)
	}
	return out
}

// Replace atomically swaps the loaded map (used by Refresh).
func (r *Registry) Replace(targets map[string]*LoadedTarget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded = targets
}
