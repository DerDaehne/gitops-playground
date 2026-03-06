package features

import (
	"github.com/DerDaehne/gitops-playground/internal/config"
	"k8s.io/client-go/kubernetes"
)

type Ingress struct {
	Config config.IngressConfig
	KubernetesClientSet kubernetes.Clientset
}

func (ingress *Ingress) Name() string {
	return "Ingress"
}

func (ingress *Ingress) IsEnabled() bool {
	return ingress.Config.Active
}

func (ingress *Ingress) Validate() error {
	return nil
}

func (ingress *Ingress) Install() error {
	println("Installing Ingress")
	return nil
}
