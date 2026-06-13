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

// stubExternalFeature is a minimal feature.Feature that also implements
// feature.ExternalConfigurator. It records every call to Install,
// Disable and ConfigureExternal so the runner test can assert which
// path was taken.
type stubExternalFeature struct {
	name             string
	enabled          bool
	installCalls     int
	disableCalls     int
	configureCalls   int
	configureErr     error
	validateCalls    int
	lastValidateBefr int // configureCalls observed before each Validate
}

func (s *stubExternalFeature) Name() string                      { return s.name }
func (s *stubExternalFeature) Order() int                        { return 50 }
func (s *stubExternalFeature) IsEnabled(_ *config.Config) bool   { return s.enabled }
func (s *stubExternalFeature) Namespace(_ *config.Config) string { return "" }
func (s *stubExternalFeature) Install(context.Context, *config.Config) error {
	s.installCalls++
	return nil
}
func (s *stubExternalFeature) Disable(context.Context, *config.Config) error {
	s.disableCalls++
	return nil
}
func (s *stubExternalFeature) Validate(context.Context, *config.Config) error {
	s.lastValidateBefr = s.configureCalls
	s.validateCalls++
	return nil
}
func (s *stubExternalFeature) ConfigureExternal(context.Context, *config.Config) error {
	s.configureCalls++
	return s.configureErr
}

// TestInstall_ExternalConfigurator_RunsWhenDisabled proves WP-B2: a
// feature that exposes ExternalConfigurator gets ConfigureExternal
// called in the IsEnabled=false branch, after Validate, and Install
// stays untouched.
func TestInstall_ExternalConfigurator_RunsWhenDisabled(t *testing.T) {
	stub := &stubExternalFeature{name: "ext", enabled: false}
	reg := feature.NewRegistry()
	reg.Add(stub)
	r := Runner{Registry: reg}

	if err := r.Install(context.Background(), &config.Config{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if stub.installCalls != 0 {
		t.Errorf("Install must not run for a disabled feature, got %d", stub.installCalls)
	}
	if stub.disableCalls != 1 {
		t.Errorf("Disable calls: want 1, got %d", stub.disableCalls)
	}
	if stub.configureCalls != 1 {
		t.Errorf("ConfigureExternal calls: want 1, got %d", stub.configureCalls)
	}
	if stub.validateCalls != 1 {
		t.Errorf("Validate calls: want 1, got %d", stub.validateCalls)
	}
	// Validate must run before ConfigureExternal: when Validate ran,
	// no ConfigureExternal call had been observed yet.
	if stub.lastValidateBefr != 0 {
		t.Errorf("Validate must run before ConfigureExternal: observed %d configure calls before Validate", stub.lastValidateBefr)
	}
}

// TestInstall_ExternalConfigurator_SkippedWhenEnabled keeps the
// internal contract intact: when the feature is enabled, we install
// it normally and do NOT fire the external hook (Install is in
// charge of the configuration in that case).
func TestInstall_ExternalConfigurator_SkippedWhenEnabled(t *testing.T) {
	stub := &stubExternalFeature{name: "ext", enabled: true}
	reg := feature.NewRegistry()
	reg.Add(stub)
	r := Runner{Registry: reg}

	if err := r.Install(context.Background(), &config.Config{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if stub.installCalls != 1 {
		t.Errorf("Install calls: want 1, got %d", stub.installCalls)
	}
	if stub.configureCalls != 0 {
		t.Errorf("ConfigureExternal must not run for an enabled feature, got %d", stub.configureCalls)
	}
}

// TestInstall_ExternalConfigurator_ErrorIsWrapped checks the runner
// surfaces the hook's error with the documented prefix.
func TestInstall_ExternalConfigurator_ErrorIsWrapped(t *testing.T) {
	boom := errors.New("kapow")
	stub := &stubExternalFeature{name: "ext", enabled: false, configureErr: boom}
	reg := feature.NewRegistry()
	reg.Add(stub)
	r := Runner{Registry: reg}

	err := r.Install(context.Background(), &config.Config{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("error chain does not contain boom: %v", err)
	}
}
