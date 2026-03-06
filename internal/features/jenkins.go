package features

import (
	"github.com/DerDaehne/gitops-playground/internal/config"
	"k8s.io/client-go/kubernetes"
)

type Jenkins struct {
	Config config.JenkinsConfig
	KubernetesClientSet kubernetes.Clientset
}

func (jenkins *Jenkins) Name() string {
	return "Jenkins"
}

func (jenkins *Jenkins) IsEnabled() bool {
	return jenkins.Config.Active
}

func (jenkins *Jenkins) Validate() error {
	return nil
}

func (jenkins *Jenkins) Install() error {
	println("Installing Jenkins")
	return nil
}
