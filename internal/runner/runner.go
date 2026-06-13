// Package runner contains Application.start() logic.
//
// Counterpart of com.cloudogu.gitops.application.Application. The Groovy
// version mixed namespace collection, secret persistence and the
// feature install loop in one method. Here we split each concern into a
// named function so error paths are explicit.
package runner

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/feature"
)

// Runner drives the install lifecycle.
type Runner struct {
	Registry *feature.Registry
	// PersistConfig persists the merged Config to a Kubernetes secret in
	// the gop namespace. May be nil in tests; the runner skips the step.
	PersistConfig func(ctx context.Context, cfg *config.Config) error
}

// Install validates every feature then installs the enabled ones in
// order. Mirrors Application.start().
func (r Runner) Install(ctx context.Context, cfg *config.Config) error {
	if r.Registry == nil {
		return fmt.Errorf("runner: registry is nil")
	}
	features := r.Registry.All()

	if err := r.collectNamespaces(cfg, features); err != nil {
		return err
	}

	if r.PersistConfig != nil {
		if err := r.PersistConfig(ctx, cfg); err != nil {
			return fmt.Errorf("persist config: %w", err)
		}
	}

	// Two-pass: validate everything first so we fail before any
	// side-effects.
	for _, f := range features {
		if v, ok := f.(feature.Validator); ok {
			if err := v.Validate(ctx, cfg); err != nil {
				return fmt.Errorf("validate %s: %w", f.Name(), err)
			}
		}
	}

	for _, f := range features {
		if !f.IsEnabled(cfg) {
			slog.Debug("feature disabled, skipping install", "name", f.Name())
			if err := f.Disable(ctx, cfg); err != nil {
				return fmt.Errorf("disable %s: %w", f.Name(), err)
			}
			// A disabled feature may still expose a post-deploy
			// configuration step for the external-provider case
			// (e.g. an external SCM-Manager that still needs the
			// namespace strategy and gitops user). Order: always
			// after Validate, never before.
			if ec, ok := f.(feature.ExternalConfigurator); ok {
				slog.Info("configuring external feature", "name", f.Name())
				if err := ec.ConfigureExternal(ctx, cfg); err != nil {
					return fmt.Errorf("configure external %s: %w", f.Name(), err)
				}
			}
			continue
		}
		slog.Info("installing feature", "name", f.Name())
		if err := f.Install(ctx, cfg); err != nil {
			return fmt.Errorf("install %s: %w", f.Name(), err)
		}
	}
	return nil
}

// collectNamespaces fills cfg.Application.Namespaces.DedicatedNamespaces
// with the unique namespaces every enabled feature owns. The Groovy code
// did this in Application.setNamespaceListToConfig().
func (r Runner) collectNamespaces(cfg *config.Config, features []feature.Feature) error {
	seen := make(map[string]struct{})
	dedicated := make([]string, 0, len(features))
	for _, f := range features {
		if !f.IsEnabled(cfg) {
			continue
		}
		ns := f.Namespace(cfg)
		if ns == "" {
			continue
		}
		if _, ok := seen[ns]; ok {
			continue
		}
		seen[ns] = struct{}{}
		dedicated = append(dedicated, ns)
	}
	cfg.Application.Namespaces.DedicatedNamespaces = dedicated
	return nil
}

// RunHook walks the registry and fires the optional config-init hooks in
// order. Counterpart of GitopsPlaygroundCli.runHook().
func (r Runner) RunHook(stage Stage, cfg *config.Config) error {
	if r.Registry == nil {
		return nil
	}
	for _, f := range r.Registry.All() {
		switch stage {
		case StagePreConfigInit:
			if h, ok := f.(feature.PreConfigInit); ok {
				if err := h.PreConfigInit(cfg); err != nil {
					return fmt.Errorf("preConfigInit %s: %w", f.Name(), err)
				}
			}
		case StagePostConfigInit:
			if h, ok := f.(feature.PostConfigInit); ok {
				if err := h.PostConfigInit(cfg); err != nil {
					return fmt.Errorf("postConfigInit %s: %w", f.Name(), err)
				}
			}
		}
	}
	return nil
}

// Stage identifies the hook point.
type Stage int

const (
	StagePreConfigInit Stage = iota
	StagePostConfigInit
)
