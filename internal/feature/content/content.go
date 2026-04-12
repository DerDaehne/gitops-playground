package content

import (
	"context"

	"github.com/DerDaehne/gitops-playground/internal/config"
	"github.com/DerDaehne/gitops-playground/internal/feature"
)

const ID feature.ID = "content"

type Feature struct {
	Config config.ContentLoaderConfig
}

func (f *Feature) ID() feature.ID         { return ID }
func (f *Feature) DependsOn() []feature.ID { return []feature.ID{"argocd", "jenkins"} }

// Enabled returns true when examples or custom repos are configured.
func (f *Feature) Enabled() bool { return f.Config.Examples || len(f.Config.Repos) > 0 }

func (f *Feature) Validate(_ context.Context) error { return nil }

func (f *Feature) Install(ctx context.Context) error {
	// TODO: implement
	return nil
}

func (f *Feature) Uninstall(ctx context.Context) error {
	// TODO: implement
	return nil
}
