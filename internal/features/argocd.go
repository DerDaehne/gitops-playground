package features

import "github.com/DerDaehne/gitops-playground/internal/config"

type ArgoCD struct {
	Config config.ArgoCDConfig
}

func (argo *ArgoCD) Name() string {
	return "ArgoCD"
}

func (argo *ArgoCD) IsEnabled() bool {
	return argo.Config.Active
}

func (argo *ArgoCD) Validate() error {
	return nil
}

func (argo *ArgoCD) Install() error {
	println("Installing ArgoCD")
	return nil
}
