package auth

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestParseBindings_Basic(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{
		"bindings.yaml": `
viewer: ["dlh-viewers"]
runner: ["dlh-runners"]
admin: ["dlh-admins"]
`,
	}}
	b, err := parseBindings(cm)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if b["dlh-runners"] != RoleRunner {
		t.Errorf("got %v", b)
	}
	if b["dlh-admins"] != RoleAdmin {
		t.Errorf("got %v", b)
	}
}

func TestResolve_HighestRoleWins(t *testing.T) {
	r := &Roles{bindings: map[string]Role{
		"a": RoleViewer,
		"b": RoleAdmin,
		"c": RoleRunner,
	}}
	id := &Identity{Groups: []string{"a", "b", "c"}}
	if got := r.Resolve(id); got != RoleAdmin {
		t.Errorf("got %v", got)
	}
}

func TestResolve_UnknownGroupsGetViewer(t *testing.T) {
	r := &Roles{bindings: map[string]Role{"a": RoleAdmin}}
	id := &Identity{Groups: []string{"unknown"}}
	if got := r.Resolve(id); got != RoleViewer {
		t.Errorf("got %v", got)
	}
}
