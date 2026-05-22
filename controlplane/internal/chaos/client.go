package chaos

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Resource is the controlplane's view of a chaos CR. The body is opaque
// on purpose — Phase C trusts the WT to supply a valid spec, since the
// WT is itself in the trusted set (managed by the platform).
type Resource struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   map[string]interface{} `json:"metadata"`
	Spec       map[string]interface{} `json:"spec"`
}

// Ref identifies a created chaos CR. Encoded as base64(JSON) so it's
// URL-safe and self-describing — no DB lookup needed for DELETE.
type Ref struct {
	Group     string `json:"g"`
	Version   string `json:"v"`
	Resource  string `json:"r"`
	Namespace string `json:"ns"`
	Name      string `json:"n"`
}

func (r Ref) Encode() string {
	b, _ := json.Marshal(r)
	return base64.RawURLEncoding.EncodeToString(b)
}

func DecodeRef(s string) (Ref, error) {
	var r Ref
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return r, fmt.Errorf("decode ref: %w", err)
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("unmarshal ref: %w", err)
	}
	return r, nil
}

// Client is the abstraction Phase D will swap for cross-cluster impls.
type Client interface {
	Create(ctx context.Context, runID string, res Resource) (Ref, error)
	Delete(ctx context.Context, ref Ref) error
	DeleteByRun(ctx context.Context, runID string) error
	ListByRun(ctx context.Context, runID string) ([]Ref, error)
	ListManaged(ctx context.Context) (map[string][]Ref, error)
}

// LocalChaosClient creates chaos CRs in the framework cluster itself.
type LocalChaosClient struct {
	Dyn       dynamic.Interface
	Namespace string
}

// Create injects the run-id label so the watchdog + DeleteByRun can find it.
func (l *LocalChaosClient) Create(ctx context.Context, runID string, res Resource) (Ref, error) {
	gvr := gvrFromAPIVersion(res.APIVersion, res.Kind)
	if gvr.Empty() {
		return Ref{}, fmt.Errorf("unsupported chaos kind: %s/%s", res.APIVersion, res.Kind)
	}
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": res.APIVersion,
		"kind":       res.Kind,
		"metadata":   res.Metadata,
		"spec":       res.Spec,
	}}
	u.SetNamespace(l.Namespace)
	labels := u.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels["dlh.run-id"] = runID
	labels["dlh.managed-by"] = "controlplane"
	u.SetLabels(labels)

	created, err := l.Dyn.Resource(gvr).Namespace(l.Namespace).Create(ctx, u, metav1.CreateOptions{})
	if err != nil {
		return Ref{}, fmt.Errorf("create %s: %w", res.Kind, err)
	}
	return Ref{
		Group:     gvr.Group,
		Version:   gvr.Version,
		Resource:  gvr.Resource,
		Namespace: l.Namespace,
		Name:      created.GetName(),
	}, nil
}

func (l *LocalChaosClient) Delete(ctx context.Context, ref Ref) error {
	gvr := schema.GroupVersionResource{Group: ref.Group, Version: ref.Version, Resource: ref.Resource}
	err := l.Dyn.Resource(gvr).Namespace(ref.Namespace).Delete(ctx, ref.Name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// DeleteByRun lists chaos resources labelled with the run id and deletes them all.
func (l *LocalChaosClient) DeleteByRun(ctx context.Context, runID string) error {
	refs, err := l.ListByRun(ctx, runID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, r := range refs {
		if err := l.Delete(ctx, r); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (l *LocalChaosClient) ListByRun(ctx context.Context, runID string) ([]Ref, error) {
	var out []Ref
	for _, gvr := range chaosGVRs() {
		list, err := l.Dyn.Resource(gvr).Namespace(l.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "dlh.run-id=" + runID,
		})
		if err != nil {
			continue // chaos kind might not be installed; tolerate
		}
		for _, item := range list.Items {
			out = append(out, Ref{
				Group:     gvr.Group,
				Version:   gvr.Version,
				Resource:  gvr.Resource,
				Namespace: l.Namespace,
				Name:      item.GetName(),
			})
		}
	}
	return out, nil
}

// ListManaged returns all chaos resources managed by the controlplane
// (label dlh.managed-by=controlplane), grouped by their dlh.run-id label.
// Used by the watchdog reconciler.
func (l *LocalChaosClient) ListManaged(ctx context.Context) (map[string][]Ref, error) {
	out := map[string][]Ref{}
	for _, gvr := range chaosGVRs() {
		list, err := l.Dyn.Resource(gvr).Namespace(l.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "dlh.managed-by=controlplane",
		})
		if err != nil {
			continue
		}
		for _, item := range list.Items {
			runID := item.GetLabels()["dlh.run-id"]
			if runID == "" {
				continue
			}
			out[runID] = append(out[runID], Ref{
				Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource,
				Namespace: l.Namespace, Name: item.GetName(),
			})
		}
	}
	return out, nil
}

// chaosGVRs returns the chaos-mesh.org kinds the controlplane manages.
// Schedule wraps the others (Plan 12), but we list direct kinds too in
// case a future WT submits one directly.
func chaosGVRs() []schema.GroupVersionResource {
	const g, v = "chaos-mesh.org", "v1alpha1"
	return []schema.GroupVersionResource{
		{Group: g, Version: v, Resource: "schedules"},
		{Group: g, Version: v, Resource: "podchaos"},
		{Group: g, Version: v, Resource: "networkchaos"},
	}
}

// gvrFromAPIVersion maps "chaos-mesh.org/v1alpha1" + "Schedule"|"PodChaos"|"NetworkChaos"
// to the matching GVR. Returns the empty GVR for unsupported kinds.
func gvrFromAPIVersion(apiVersion, kind string) schema.GroupVersionResource {
	// Lowercase Kind → resource lookup. chaos-mesh.org kinds:
	//   Schedule → schedules ; PodChaos → podchaos ; NetworkChaos → networkchaos.
	resource := strings.ToLower(kind)
	if resource == "schedule" {
		resource = "schedules"
	}
	for _, gvr := range chaosGVRs() {
		if apiVersion == gvr.Group+"/"+gvr.Version && gvr.Resource == resource {
			return gvr
		}
	}
	return schema.GroupVersionResource{}
}
