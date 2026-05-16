// Package publish writes the verdict summary to a Kubernetes ConfigMap.
package publish

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dlh/dlh-test-fw/verdict-job/internal/eval"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Publisher struct {
	Cs        kubernetes.Interface
	Namespace string
}

func (p *Publisher) Publish(ctx context.Context, workflow string, r *eval.Result) error {
	name := "dlh-result-" + workflow
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.Namespace,
			Labels: map[string]string{"app.kubernetes.io/managed-by": "dlh-verdict", "dlh.workflow": workflow}},
		Data: map[string]string{"result.json": string(body)},
	}
	_, err = p.Cs.CoreV1().ConfigMaps(p.Namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("publish: create: %w", err)
	}
	_, err = p.Cs.CoreV1().ConfigMaps(p.Namespace).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("publish: update: %w", err)
	}
	return nil
}
