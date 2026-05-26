// Package priorities reads + writes the dlh-scenario-priorities ConfigMap:
// per-scenario default priority overrides consulted by the submitter.
package priorities

import (
	"context"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Store is a thin accessor over the dlh-scenario-priorities ConfigMap.
type Store struct {
	Client    kubernetes.Interface
	Namespace string
	Name      string
}

// Get returns the override priority for a scenario and whether one is set.
func (s *Store) Get(ctx context.Context, scenario string) (int, bool, error) {
	all, err := s.All(ctx)
	if err != nil {
		return 0, false, err
	}
	v, ok := all[scenario]
	return v, ok, nil
}

// All returns every parseable override (non-integer values are skipped).
func (s *Store) All(ctx context.Context) (map[string]int, error) {
	cm, err := s.Client.CoreV1().ConfigMaps(s.Namespace).Get(ctx, s.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return map[string]int{}, nil // absent CM = no overrides
		}
		return nil, err
	}
	out := make(map[string]int, len(cm.Data))
	for k, v := range cm.Data {
		if n, err := strconv.Atoi(v); err == nil {
			out[k] = n
		}
	}
	return out, nil
}

// Set writes (creating the CM if absent) the override for a scenario.
func (s *Store) Set(ctx context.Context, scenario string, priority int) error {
	cms := s.Client.CoreV1().ConfigMaps(s.Namespace)
	cm, err := cms.Get(ctx, s.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: s.Name, Namespace: s.Namespace},
			Data:       map[string]string{},
		}
		cm.Data[scenario] = strconv.Itoa(priority)
		_, cErr := cms.Create(ctx, cm, metav1.CreateOptions{})
		return cErr
	}
	if err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[scenario] = strconv.Itoa(priority)
	_, uErr := cms.Update(ctx, cm, metav1.UpdateOptions{})
	return uErr
}
