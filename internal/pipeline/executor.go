package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/DerDaehne/gitops-playground/internal/feature"
)

// Executor runs features in dependency order, parallelising independent features.
type Executor struct{}

// Apply validates all enabled features, then installs them level by level.
// Features within a level run in parallel goroutines.
func (e *Executor) Apply(ctx context.Context, features []feature.Feature) error {
	enabled := make([]feature.Feature, 0, len(features))
	for _, f := range features {
		if f.Enabled() {
			enabled = append(enabled, f)
		}
	}

	dag, err := Build(enabled)
	if err != nil {
		return fmt.Errorf("building feature DAG: %w", err)
	}

	levels, err := dag.Levels()
	if err != nil {
		return err
	}

	slog.Info("Validating features...")
	for _, f := range enabled {
		if err := f.Validate(ctx); err != nil {
			return fmt.Errorf("feature %q validation failed: %w", f.ID(), err)
		}
	}

	slog.Info("Installing features...", "total", len(enabled))
	for i, level := range levels {
		slog.Debug("Executing DAG level", "level", i+1, "features", len(level))
		if err := runLevel(ctx, level); err != nil {
			return err
		}
	}
	slog.Info("All features installed successfully.")
	return nil
}

// Destroy uninstalls features in reverse dependency order.
func (e *Executor) Destroy(ctx context.Context, features []feature.Feature) error {
	enabled := make([]feature.Feature, 0, len(features))
	for _, f := range features {
		if f.Enabled() {
			enabled = append(enabled, f)
		}
	}

	dag, err := Build(enabled)
	if err != nil {
		return fmt.Errorf("building feature DAG: %w", err)
	}

	levels, err := dag.Levels()
	if err != nil {
		return err
	}

	// Uninstall in reverse order
	for i := len(levels) - 1; i >= 0; i-- {
		if err := runLevelUninstall(ctx, levels[i]); err != nil {
			return err
		}
	}
	return nil
}

func runLevel(ctx context.Context, features []feature.Feature) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(features))

	for _, f := range features {
		wg.Add(1)
		go func(f feature.Feature) {
			defer wg.Done()
			slog.Info("Installing", "feature", f.ID())
			if err := f.Install(ctx); err != nil {
				errs <- fmt.Errorf("feature %q install failed: %w", f.ID(), err)
			}
		}(f)
	}

	wg.Wait()
	close(errs)

	var combined error
	for err := range errs {
		if combined == nil {
			combined = err
		} else {
			combined = fmt.Errorf("%w; %v", combined, err)
		}
	}
	return combined
}

func runLevelUninstall(ctx context.Context, features []feature.Feature) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(features))

	for _, f := range features {
		wg.Add(1)
		go func(f feature.Feature) {
			defer wg.Done()
			slog.Info("Uninstalling", "feature", f.ID())
			if err := f.Uninstall(ctx); err != nil {
				errs <- fmt.Errorf("feature %q uninstall failed: %w", f.ID(), err)
			}
		}(f)
	}

	wg.Wait()
	close(errs)

	var combined error
	for err := range errs {
		if combined == nil {
			combined = err
		} else {
			combined = fmt.Errorf("%w; %v", combined, err)
		}
	}
	return combined
}
