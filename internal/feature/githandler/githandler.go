package githandler

import (
	"context"

	"github.com/DerDaehne/gitops-playground/internal/config"
	"github.com/DerDaehne/gitops-playground/internal/feature"
)

// Phase 1 — deployed imperatively, not via ArgoCD.

const ID feature.ID = "githandler"

type Feature struct {
	Config *config.Config
}

func (f *Feature) ID() feature.ID         { return ID }
func (f *Feature) DependsOn() []feature.ID { return nil }

// Enabled always returns true — GitHandler is always required.
func (f *Feature) Enabled() bool { return true }

func (f *Feature) Validate(_ context.Context) error { return nil }

func (f *Feature) Install(ctx context.Context) error {
	// TODO: implement
	return nil
}

func (f *Feature) Uninstall(ctx context.Context) error {
	// TODO: implement
	return nil
}
