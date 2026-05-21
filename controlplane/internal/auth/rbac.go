package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Role is a coarse permission level.
type Role string

const (
	RoleViewer Role = "viewer"
	RoleRunner Role = "runner"
	RoleAdmin  Role = "admin"
)

var roleOrder = map[Role]int{RoleViewer: 1, RoleRunner: 2, RoleAdmin: 3}

// IsAtLeast returns true if r grants at least the privileges of want.
func (r Role) IsAtLeast(want Role) bool {
	return roleOrder[r] >= roleOrder[want]
}

// Roles maps OIDC groups to a Role. Loaded from a ConfigMap at startup;
// caller may set up a goroutine to call Refresh periodically.
type Roles struct {
	mu       sync.RWMutex
	bindings map[string]Role // group -> role
}

// NewRoles fetches the configmap once.
func NewRoles(ctx context.Context, client kubernetes.Interface, ns, name string) (*Roles, error) {
	r := &Roles{bindings: map[string]Role{}}
	if err := r.Refresh(ctx, client, ns, name); err != nil {
		return nil, err
	}
	return r, nil
}

// Refresh re-reads the ConfigMap. Expected data shape:
//
//	data:
//	  bindings.yaml: |
//	    viewer: ["dlh-viewers"]
//	    runner: ["dlh-runners"]
//	    admin:  ["dlh-admins"]
func (r *Roles) Refresh(ctx context.Context, client kubernetes.Interface, ns, name string) error {
	cm, err := client.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get configmap %s/%s: %w", ns, name, err)
	}
	bindings, err := parseBindings(cm)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.bindings = bindings
	r.mu.Unlock()
	return nil
}

func parseBindings(cm *corev1.ConfigMap) (map[string]Role, error) {
	raw, ok := cm.Data["bindings.yaml"]
	if !ok {
		return nil, errors.New("configmap missing bindings.yaml key")
	}
	// Tiny hand-rolled parser to avoid pulling in yaml.v3 for such a small file.
	out := map[string]Role{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon == -1 {
			continue
		}
		role := Role(strings.TrimSpace(line[:colon]))
		rest := strings.TrimSpace(line[colon+1:])
		rest = strings.TrimPrefix(rest, "[")
		rest = strings.TrimSuffix(rest, "]")
		for _, g := range strings.Split(rest, ",") {
			g = strings.TrimSpace(g)
			g = strings.Trim(g, "\"")
			if g == "" {
				continue
			}
			out[g] = role
		}
	}
	return out, nil
}

// Resolve returns the highest role across the identity's groups, or
// RoleViewer if none match (authenticated-but-unknown == viewer).
func (r *Roles) Resolve(id *Identity) Role {
	r.mu.RLock()
	defer r.mu.RUnlock()
	best := RoleViewer
	for _, g := range id.Groups {
		if role, ok := r.bindings[g]; ok {
			if role.IsAtLeast(best) {
				best = role
			}
		}
	}
	return best
}
