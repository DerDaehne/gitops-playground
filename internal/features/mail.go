package features

import (
	"github.com/DerDaehne/gitops-playground/internal/config"
	"k8s.io/client-go/kubernetes"
)

type Mail struct {
	Config config.MailConfig
	KubernetesClientSet kubernetes.Clientset
}

func (mail *Mail) Name() string {
	return "Mail"
}

func (mail *Mail) IsEnabled() bool {
	return mail.Config.Active
}

func (mail *Mail) Validate() error {
	return nil
}

func (mail *Mail) Install() error {
	println("Installing Mail")
	return nil
}
