// Package chaosresult reads ChaosResult.status.experimentStatus.verdict from the Litmus CRD.
package chaosresult

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type Client struct {
	Dyn          dynamic.Interface
	GVR          schema.GroupVersionResource
	Namespace    string
	PollInterval time.Duration // default 2s
}

// GetVerdict reads the named ChaosResult. While status reads "Awaited", retries until timeout.
// Returns "Pass", "Fail", "Stopped", etc. (whatever Litmus wrote).
func (c *Client) GetVerdict(ctx context.Context, name string, timeout time.Duration) (string, error) {
	interval := c.PollInterval
	if interval == 0 {
		interval = 2 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		u, err := c.Dyn.Resource(c.GVR).Namespace(c.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("chaosresult: get %s: %w", name, err)
		}
		v, _ := nested(u, "status", "experimentStatus", "verdict")
		s, _ := v.(string)
		if s != "" && s != "Awaited" {
			return s, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("chaosresult: %s still %q after %v", name, s, timeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
	}
}

func nested(u *unstructured.Unstructured, path ...string) (any, bool) {
	var cur any = u.Object
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}
