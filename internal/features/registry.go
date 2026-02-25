package features

import "github.com/DerDaehne/gitops-playground/internal/config"

type Registry struct {
	Config config.RegistryConfig
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
	println("Installing Registry")
	return nil
}
