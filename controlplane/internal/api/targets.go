package api

import (
	"github.com/dlh/dlh-test-fw/controlplane/internal/api/gen"
	"github.com/dlh/dlh-test-fw/controlplane/internal/targets"
)

// targetDTO converts an in-memory LoadedTarget to the OpenAPI Target.
// All optional fields on gen.Target are *T pointers per codegen output.
func targetDTO(t *targets.LoadedTarget) gen.Target {
	allowed := append([]string(nil), t.AllowedTargetTypes...)
	displayName := t.DisplayName
	kc := t.KubeconfigSecret
	ns := t.Namespace
	configured := t.RestConfig != nil
	return gen.Target{
		Id:                 t.ID,
		DisplayName:        &displayName,
		KubeconfigSecret:   &kc,
		AllowedTargetTypes: &allowed,
		Namespace:          &ns,
		Configured:         &configured,
	}
}
