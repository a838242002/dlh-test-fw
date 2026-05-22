package chaos

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// RemoteChaosClient creates chaos CRs in a target cluster identified by
// the kubeconfig that built RestConfig. Namespace is the chaos namespace
// on the remote cluster.
type RemoteChaosClient struct {
	RestConfig *rest.Config
	Namespace  string
	// TargetID is stored on labels for watchdog reconciliation.
	TargetID string
}

func (r *RemoteChaosClient) dyn() (dynamic.Interface, error) {
	if r.RestConfig == nil {
		return nil, fmt.Errorf("remote chaos client: no kubeconfig (target %q)", r.TargetID)
	}
	return dynamic.NewForConfig(r.RestConfig)
}

func (r *RemoteChaosClient) Create(ctx context.Context, runID string, res Resource) (Ref, error) {
	gvr := gvrFromAPIVersion(res.APIVersion, res.Kind)
	if gvr.Empty() {
		return Ref{}, fmt.Errorf("unsupported chaos kind: %s/%s", res.APIVersion, res.Kind)
	}
	dyn, err := r.dyn()
	if err != nil {
		return Ref{}, err
	}
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": res.APIVersion,
		"kind":       res.Kind,
		"metadata":   res.Metadata,
		"spec":       res.Spec,
	}}
	u.SetNamespace(r.Namespace)
	labels := u.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels["dlh.run-id"] = runID
	labels["dlh.managed-by"] = "controlplane"
	labels["dlh.target"] = r.TargetID
	u.SetLabels(labels)
	created, err := dyn.Resource(gvr).Namespace(r.Namespace).Create(ctx, u, metav1.CreateOptions{})
	if err != nil {
		return Ref{}, fmt.Errorf("create %s on target %s: %w", res.Kind, r.TargetID, err)
	}
	return Ref{
		Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource,
		Namespace: r.Namespace, Name: created.GetName(),
	}, nil
}

func (r *RemoteChaosClient) Delete(ctx context.Context, ref Ref) error {
	dyn, err := r.dyn()
	if err != nil {
		return err
	}
	gvr := schema.GroupVersionResource{Group: ref.Group, Version: ref.Version, Resource: ref.Resource}
	err = dyn.Resource(gvr).Namespace(ref.Namespace).Delete(ctx, ref.Name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *RemoteChaosClient) DeleteByRun(ctx context.Context, runID string) error {
	refs, err := r.ListByRun(ctx, runID)
	if err != nil {
		return err
	}
	var firstErr error
	for _, ref := range refs {
		if err := r.Delete(ctx, ref); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *RemoteChaosClient) ListByRun(ctx context.Context, runID string) ([]Ref, error) {
	dyn, err := r.dyn()
	if err != nil {
		return nil, err
	}
	var out []Ref
	for _, gvr := range chaosGVRs() {
		list, err := dyn.Resource(gvr).Namespace(r.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "dlh.run-id=" + runID,
		})
		if err != nil {
			continue
		}
		for _, item := range list.Items {
			out = append(out, Ref{
				Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource,
				Namespace: r.Namespace, Name: item.GetName(),
			})
		}
	}
	return out, nil
}

func (r *RemoteChaosClient) ListManaged(ctx context.Context) (map[string][]Ref, error) {
	dyn, err := r.dyn()
	if err != nil {
		return nil, err
	}
	out := map[string][]Ref{}
	for _, gvr := range chaosGVRs() {
		list, err := dyn.Resource(gvr).Namespace(r.Namespace).List(ctx, metav1.ListOptions{
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
				Namespace: r.Namespace, Name: item.GetName(),
			})
		}
	}
	return out, nil
}

// Compile-time interface conformance check.
var _ Client = (*RemoteChaosClient)(nil)
