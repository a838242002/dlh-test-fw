package targets

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ProbeResult is what the /api/targets/{id}/test endpoint returns.
type ProbeResult struct {
	OK      bool          `json:"ok"`
	Latency time.Duration `json:"latencyNanos"`
	Error   string        `json:"error,omitempty"`
}

// Probe makes a list-with-limit=1 call against chaos-mesh.org/v1alpha1
// schedules. Confirms (a) the kubeconfig parses, (b) the cluster API is
// reachable, (c) the SA can read chaos resources. Cheap.
func Probe(ctx context.Context, t *LoadedTarget) ProbeResult {
	if t == nil || t.RestConfig == nil {
		return ProbeResult{OK: false, Error: "target has no kubeconfig"}
	}
	dyn, err := dynamic.NewForConfig(t.RestConfig)
	if err != nil {
		return ProbeResult{OK: false, Error: "dynamic client: " + err.Error()}
	}
	gvr := schema.GroupVersionResource{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "schedules"}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	_, err = dyn.Resource(gvr).Namespace(t.Namespace).List(probeCtx, metav1.ListOptions{Limit: 1})
	elapsed := time.Since(start)
	if err != nil {
		return ProbeResult{OK: false, Latency: elapsed, Error: err.Error()}
	}
	return ProbeResult{OK: true, Latency: elapsed}
}
