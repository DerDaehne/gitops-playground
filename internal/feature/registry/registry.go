package registry

import (
	"context"

	"github.com/DerDaehne/gitops-playground/internal/config"
	"github.com/DerDaehne/gitops-playground/internal/feature"
	"github.com/DerDaehne/gitops-playground/internal/helm"
	"github.com/DerDaehne/gitops-playground/internal/k8s"
)

const ID feature.ID = "registry"

type Feature struct {
	Config     config.RegistryConfig
	K8sClient  *k8s.Client
	HelmClient *helm.Client
}

func (f *Feature) ID() feature.ID         { return ID }
func (f *Feature) DependsOn() []feature.ID { return []feature.ID{"argocd"} }
func (f *Feature) Enabled() bool           { return f.Config.Active }

func (f *Feature) Validate(_ context.Context) error { return nil }

func (f *Feature) Install(ctx context.Context) error {
	// TODO: deploy registry helm chart via ArgoCD Application
	return nil
}

func (f *Feature) Uninstall(ctx context.Context) error {
	// TODO: remove ArgoCD Application
	return nil
}
