package api

import (
	"context"
	"sort"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/dlh/dlh-test-fw/controlplane/internal/queue"
)

// ConfigMapLocks reads semaphore keys + slot counts from the dlh-scenario-locks
// ConfigMap. Keys are returned sorted for stable lane ordering.
type ConfigMapLocks struct {
	Client    kubernetes.Interface
	Namespace string
	Name      string
}

func (c *ConfigMapLocks) Keys(ctx context.Context) ([]queue.LockKey, error) {
	cm, err := c.Client.CoreV1().ConfigMaps(c.Namespace).Get(ctx, c.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	keys := make([]queue.LockKey, 0, len(cm.Data))
	for k, v := range cm.Data {
		slots, _ := strconv.Atoi(v)
		if slots <= 0 {
			slots = 1
		}
		keys = append(keys, queue.LockKey{Key: k, Slots: slots})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Key < keys[j].Key })
	return keys, nil
}
