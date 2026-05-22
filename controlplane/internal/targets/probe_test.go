package targets

import (
	"context"
	"testing"
)

func TestProbe_NilTarget(t *testing.T) {
	res := Probe(context.Background(), nil)
	if res.OK {
		t.Errorf("nil target should return OK=false")
	}
}

func TestProbe_NoKubeconfig(t *testing.T) {
	res := Probe(context.Background(), &LoadedTarget{Target: Target{ID: "x"}})
	if res.OK {
		t.Errorf("missing kubeconfig should return OK=false")
	}
	if res.Error == "" {
		t.Errorf("expected error message, got empty")
	}
}
