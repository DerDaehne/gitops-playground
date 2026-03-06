package features

import (
	"github.com/DerDaehne/gitops-playground/internal/config"
	"k8s.io/client-go/kubernetes"
)

type CertManager struct {
	Config config.CertManagerConfig
	KubernetesClientSet kubernetes.Clientset
}

func (certmanager *CertManager) Name() string {
	return "CertManager"
}

func (certmanager *CertManager) IsEnabled() bool {
	return certmanager.Config.Active
}

func (certmanager *CertManager) Validate() error {
	return nil
}

func (certmanager *CertManager) Install() error {
	println("Installing CertManager")
	return nil
}
