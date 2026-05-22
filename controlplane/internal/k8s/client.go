package k8s

import (
	"fmt"

	wfclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Clients groups the Kubernetes + Argo client-go clients we need.
type Clients struct {
	Core    kubernetes.Interface
	Argo    wfclient.Interface
	Dynamic dynamic.Interface
}

// NewClients builds an in-cluster client if kubeconfigPath is empty,
// else uses the kubeconfig file at that path. The two code paths share
// the resulting rest.Config.
func NewClients(kubeconfigPath string) (*Clients, error) {
	var cfg *rest.Config
	var err error
	if kubeconfigPath != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("build rest.Config: %w", err)
	}
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("core client: %w", err)
	}
	argo, err := wfclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("argo client: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	return &Clients{Core: core, Argo: argo, Dynamic: dyn}, nil
}
