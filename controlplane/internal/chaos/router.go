package chaos

import (
	"context"
	"fmt"

	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
)

// Router picks Local or Remote chaos client per call based on targetID.
// Implements the Client interface so existing callers (handlers + watchdog)
// can use it transparently.
//
// The Client interface methods don't carry a targetID; we add targetID-aware
// methods on Router for handler use. Callers that need targeting use
// CreateForTarget; the existing methods route to Local (empty target).
type Router struct {
	Local    Client
	Registry *targets.Registry
}

// CreateForTarget routes by targetID. Empty targetID = local.
func (r *Router) CreateForTarget(ctx context.Context, runID, targetID string, res Resource) (Ref, error) {
	c, err := r.pick(targetID)
	if err != nil {
		return Ref{}, err
	}
	return c.Create(ctx, runID, res)
}

// DeleteForTarget routes by targetID. Empty = local.
func (r *Router) DeleteForTarget(ctx context.Context, targetID string, ref Ref) error {
	c, err := r.pick(targetID)
	if err != nil {
		return err
	}
	return c.Delete(ctx, ref)
}

// Existing Client-interface methods route to Local. The watchdog will use
// ListManaged across all targets via the fan-out helpers below.
func (r *Router) Create(ctx context.Context, runID string, res Resource) (Ref, error) {
	return r.Local.Create(ctx, runID, res)
}
func (r *Router) Delete(ctx context.Context, ref Ref) error {
	return r.Local.Delete(ctx, ref)
}
func (r *Router) DeleteByRun(ctx context.Context, runID string) error {
	// Best-effort cross-cluster cleanup: walk every known client.
	var firstErr error
	for _, c := range r.allClients() {
		if err := c.DeleteByRun(ctx, runID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
func (r *Router) ListByRun(ctx context.Context, runID string) ([]Ref, error) {
	var out []Ref
	for _, c := range r.allClients() {
		refs, err := c.ListByRun(ctx, runID)
		if err != nil {
			continue
		}
		out = append(out, refs...)
	}
	return out, nil
}
func (r *Router) ListManaged(ctx context.Context) (map[string][]Ref, error) {
	out := map[string][]Ref{}
	for _, c := range r.allClients() {
		got, err := c.ListManaged(ctx)
		if err != nil {
			continue
		}
		for k, v := range got {
			out[k] = append(out[k], v...)
		}
	}
	return out, nil
}

func (r *Router) pick(targetID string) (Client, error) {
	if targetID == "" {
		return r.Local, nil
	}
	if r.Registry == nil {
		return nil, fmt.Errorf("router: no registry for target %q", targetID)
	}
	t := r.Registry.Get(targetID)
	if t == nil {
		return nil, fmt.Errorf("router: unknown target %q", targetID)
	}
	return &RemoteChaosClient{
		RestConfig: t.RestConfig,
		Namespace:  t.Namespace,
		TargetID:   t.ID,
	}, nil
}

func (r *Router) allClients() []Client {
	clients := []Client{r.Local}
	if r.Registry == nil {
		return clients
	}
	for _, t := range r.Registry.List() {
		if t.RestConfig == nil {
			continue
		}
		clients = append(clients, &RemoteChaosClient{
			RestConfig: t.RestConfig,
			Namespace:  t.Namespace,
			TargetID:   t.ID,
		})
	}
	return clients
}

// Compile-time check
var _ Client = (*Router)(nil)
