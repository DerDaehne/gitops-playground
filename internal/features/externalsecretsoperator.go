package features

import (
	"github.com/DerDaehne/gitops-playground/internal/config"
	"k8s.io/client-go/kubernetes"
)

type ExternalSecretsOperator struct {
	Config config.SecretsConfig
	KubernetesClientSet kubernetes.Clientset
}

func (eso *ExternalSecretsOperator) Name() string {
	return "ExternalSecretsOperator"
}

func (eso *ExternalSecretsOperator) IsEnabled() bool {
	return eso.Config.Active
}

func (eso *ExternalSecretsOperator) Validate() error {
	return nil
}

func (eso *ExternalSecretsOperator) Install() error {
	println("Installing ExternalSecretsOperator")
	return nil
}
