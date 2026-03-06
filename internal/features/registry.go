package features

import (
	"github.com/DerDaehne/gitops-playground/internal/config"
	"github.com/DerDaehne/gitops-playground/internal/helm"
	"github.com/DerDaehne/gitops-playground/internal/kubeutils"
	"helm.sh/helm/v3/pkg/chartutil"
	"k8s.io/client-go/kubernetes"
)

type Registry struct {
	Config config.RegistryConfig
	KubernetesClientSet kubernetes.Clientset
	NamePrefix string
}

func (registry *Registry) Name() string {
	return "Registry"
}

func (registry *Registry) IsEnabled() bool {
	return registry.Config.Active
}

func (registry *Registry) Validate() error {
	return nil
}

func (registry *Registry) Install() error {
	var namespace string = "registry"

	if registry.Config.Internal {
		namespace = registry.NamePrefix + "registry"
	}

	if registry.Config.Helm.Values == nil { registry.Config.Helm.Values = chartutil.Values{}}
	registry.Config.Helm.Values["service"] = map[string]interface{}{
		"nodePort": 30000,
		"type": "NodePort",
	}

	if registry.Config.InternalPort != 30000 {
		kubeutils.CreateNodePortServiceTCP(&registry.KubernetesClientSet, namespace, registry.Config.Helm.Chart, "docker-registry-internal-port", 5000, registry.Config.InternalPort)
	}
	
	_, err := helm.DeployHelmChart(registry.Config.Helm, namespace, "registry")
	if err != nil { return err }
	return nil
}
