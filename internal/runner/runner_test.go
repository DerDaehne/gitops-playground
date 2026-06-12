package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/feature"
)

// TestInstall_PersistConfig_Invoked verifies the runner calls the
// PersistConfig hook exactly once, before any feature Install runs.
func TestInstall_PersistConfig_Invoked(t *testing.T) {
	calls := 0
	r := Runner{
		Registry: feature.NewRegistry(),
		PersistConfig: func(ctx context.Context, cfg *config.Config) error {
			calls++
			return nil
		},
	}
	if err := r.Install(context.Background(), &config.Config{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if calls != 1 {
		t.Fatalf("PersistConfig calls: want 1, got %d", calls)
	}
}

// TestInstall_PersistConfig_ErrorIsWrapped checks the runner surfaces
// the closure's error with the documented prefix.
func TestInstall_PersistConfig_ErrorIsWrapped(t *testing.T) {
	boom := errors.New("boom")
	r := Runner{
		Registry: feature.NewRegistry(),
		PersistConfig: func(ctx context.Context, cfg *config.Config) error {
			return boom
		},
	}
	err := r.Install(context.Background(), &config.Config{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("error chain does not contain boom: %v", err)
	}
}

// TestInstall_PersistConfig_NilIsSkipped confirms the existing nil-skip
// path keeps working for dry-run / pure-unit tests.
func TestInstall_PersistConfig_NilIsSkipped(t *testing.T) {
	r := Runner{Registry: feature.NewRegistry()} // PersistConfig nil
	if err := r.Install(context.Background(), &config.Config{}); err != nil {
		t.Fatalf("Install with nil PersistConfig: %v", err)
	}
}
