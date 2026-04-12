package ingress

import (
	"context"

	"github.com/DerDaehne/gitops-playground/internal/config"
	"github.com/DerDaehne/gitops-playground/internal/feature"
)

const ID feature.ID = "ingress"

type Feature struct {
	Config config.IngressConfig
}

func (f *Feature) ID() feature.ID         { return ID }
func (f *Feature) DependsOn() []feature.ID { return []feature.ID{"argocd"} }
func (f *Feature) Enabled() bool           { return f.Config.Active }

func (f *Feature) Validate(_ context.Context) error { return nil }

func (f *Feature) Install(ctx context.Context) error {
	// TODO: implement
	return nil
}

func (f *Feature) Uninstall(ctx context.Context) error {
	// TODO: implement
	return nil
}
