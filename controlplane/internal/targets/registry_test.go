package targets

import "testing"

func TestRegistry_GetAndList(t *testing.T) {
	r := NewRegistry()
	if r.Get("nope") != nil {
		t.Errorf("Get on empty registry should be nil")
	}
	r.Replace(map[string]*LoadedTarget{
		"a": {Target: Target{ID: "a"}},
		"b": {Target: Target{ID: "b"}},
	})
	if r.Get("a") == nil || r.Get("b") == nil {
		t.Errorf("Get missed populated targets")
	}
	if r.Get("c") != nil {
		t.Errorf("Get on unknown id should be nil")
	}
	if len(r.List()) != 2 {
		t.Errorf("List length: %d", len(r.List()))
	}
}
