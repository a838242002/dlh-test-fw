// Package targets owns the registry of remote target clusters that the
// controlplane can inject chaos into. Definitions are loaded from a
// Kubernetes ConfigMap (dlh-targets) + per-target kubeconfig Secrets,
// both Argo-CD-synced. The registry refreshes every 30s by re-reading
// both resources and atomically swapping the cache.
package targets

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
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

// Loader fetches the dlh-targets ConfigMap and per-target Secrets and
// builds a fresh LoadedTarget map.
type Loader struct {
	Client    kubernetes.Interface
	Namespace string
	// ConfigMapName defaults to "dlh-targets".
	ConfigMapName string
}

// Load reads the configmap + secrets and returns the current target set.
func (l *Loader) Load(ctx context.Context) (map[string]*LoadedTarget, error) {
	cmName := l.ConfigMapName
	if cmName == "" {
		cmName = "dlh-targets"
	}
	cm, err := l.Client.CoreV1().ConfigMaps(l.Namespace).Get(ctx, cmName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get configmap %s/%s: %w", l.Namespace, cmName, err)
	}
	rawYAML, ok := cm.Data["targets.yaml"]
	if !ok {
		// Empty registry — chart ships an empty default, this is fine.
		return map[string]*LoadedTarget{}, nil
	}
	var doc struct {
		Targets []Target `yaml:"targets"`
	}
	if err := yaml.Unmarshal([]byte(rawYAML), &doc); err != nil {
		return nil, fmt.Errorf("parse targets.yaml: %w", err)
	}
	out := map[string]*LoadedTarget{}
	for i := range doc.Targets {
		t := doc.Targets[i]
		if t.ID == "" || t.KubeconfigSecret == "" {
			continue // skip malformed entries
		}
		if t.Namespace == "" {
			t.Namespace = "dlh-test-fw"
		}
		if t.DisplayName == "" {
			t.DisplayName = t.ID
		}
		now := metav1.Now().Time
		cfg, kcErr := l.loadKubeconfig(ctx, t.KubeconfigSecret)
		if kcErr != nil {
			// Don't fail the whole load — surface partial results so a
			// single broken secret doesn't disable the registry.
			out[t.ID] = &LoadedTarget{Target: t, LastSeen: now}
			continue
		}
		out[t.ID] = &LoadedTarget{Target: t, RestConfig: cfg, LastSeen: now}
	}
	return out, nil
}

func (l *Loader) loadKubeconfig(ctx context.Context, secretName string) (*rest.Config, error) {
	sec, err := l.Client.CoreV1().Secrets(l.Namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", l.Namespace, secretName, err)
	}
	raw, ok := sec.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("secret %s missing 'kubeconfig' key", secretName)
	}
	cfg, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	return cfg, nil
}

// Refresher repeatedly Loads + Replaces into the registry every interval
// until ctx is cancelled. First refresh runs synchronously so the registry
// is populated before Run returns control to its goroutine.
type Refresher struct {
	Loader   *Loader
	Registry *Registry
	Interval time.Duration // defaults to 30s
}

func (r *Refresher) Run(ctx context.Context) {
	interval := r.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}
	// First tick immediately so the registry is populated before Run
	// returns control to the caller's goroutine.
	r.tick(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.tick(ctx)
		}
	}
}

func (r *Refresher) tick(ctx context.Context) {
	loaded, err := r.Loader.Load(ctx)
	if err != nil {
		slog.Warn("targets refresh failed", "err", err)
		return
	}
	r.Registry.Replace(loaded)
}
