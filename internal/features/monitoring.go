package features

import (
	"github.com/DerDaehne/gitops-playground/internal/config"
	"k8s.io/client-go/kubernetes"
)

type Monitoring struct {
	Config config.MonitoringConfig
	KubernetesClientSet kubernetes.Clientset
}

func (monitoring *Monitoring) Name() string {
	return "Monitoring"
}

func (monitoring *Monitoring) IsEnabled() bool {
	return monitoring.Config.Active
}

func (monitoring *Monitoring) Validate() error {
	return nil
}

func (monitoring *Monitoring) Install() error {
	println("Installing Monitoring")
	return nil
}
