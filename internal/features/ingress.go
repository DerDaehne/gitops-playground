package features

import "github.com/DerDaehne/gitops-playground/internal/config"

type Ingress struct {
	Config config.IngressConfig
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
