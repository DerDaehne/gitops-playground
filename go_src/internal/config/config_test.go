package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewHasDefaults(t *testing.T) {
	c := New()
	if c.Jenkins.Helm.Chart != "jenkins" {
		t.Errorf("jenkins helm chart default: got %q", c.Jenkins.Helm.Chart)
	}
	if c.Registry.InternalPort != DefaultRegistryPort {
		t.Errorf("registry port default: got %d, want %d", c.Registry.InternalPort, DefaultRegistryPort)
	}
	if c.Application.GitName != "Cloudogu" {
		t.Errorf("git name default: got %q", c.Application.GitName)
	}
}

func TestApplicationSchemaTenantName(t *testing.T) {
	cases := []struct {
		prefix string
		want   string
	}{
		{"", ""},
		{"tenant-", "tenant"},
		{"abc", "abc"},
	}
	for _, c := range cases {
		got := ApplicationSchema{NamePrefix: c.prefix}.TenantName()
		if got != c.want {
			t.Errorf("TenantName(%q)=%q want %q", c.prefix, got, c.want)
		}
	}
}

func TestLoadProfileMinimal(t *testing.T) {
	cfg, err := Load(context.Background(), LoadOptions{ProfileName: "minimal"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Features.ArgoCD.Active {
		t.Errorf("argocd should be active after loading the minimal profile")
	}
	if cfg.Application.BaseURL != "http://localhost" {
		t.Errorf("baseUrl from profile: %q", cfg.Application.BaseURL)
	}
	if !cfg.Application.Yes {
		t.Errorf("yes flag from profile not picked up")
	}
}

func TestLoadProfileNotFound(t *testing.T) {
	_, err := Load(context.Background(), LoadOptions{ProfileName: "does-not-exist"})
	if err == nil {
		t.Fatalf("expected error for missing profile")
	}
}

func TestLoadConfigFileOverridesProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.yaml")
	if err := os.WriteFile(path, []byte("application:\n  baseUrl: http://override.example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(context.Background(), LoadOptions{
		ProfileName: "minimal",
		ConfigFiles: []string{path},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Application.BaseURL != "http://override.example" {
		t.Errorf("baseUrl should be overridden by config file, got %q", cfg.Application.BaseURL)
	}
	// profile values that were not touched by the file must survive
	if !cfg.Features.ArgoCD.Active {
		t.Errorf("argocd active should be preserved after merge")
	}
}

func TestLoadConfigMapRequiresK8s(t *testing.T) {
	_, err := Load(context.Background(), LoadOptions{ConfigMaps: []string{"my-map"}})
	if err == nil || !strings.Contains(err.Error(), "Kubernetes connection") {
		t.Errorf("expected error about missing K8s connection, got %v", err)
	}
}

type stubConfigMapReader struct {
	yaml string
	err  error
}

func (s *stubConfigMapReader) GetConfigMap(_ context.Context, _, _ string) (string, error) {
	return s.yaml, s.err
}

func TestLoadConfigMapMergesYAML(t *testing.T) {
	r := &stubConfigMapReader{yaml: "features:\n  monitoring:\n    active: true\n"}
	cfg, err := Load(context.Background(), LoadOptions{
		ConfigMaps: []string{"x"},
		K8s:        r,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Features.Monitoring.Active {
		t.Errorf("monitoring should be active after config map load")
	}
}

func TestLoadConfigMapErrorPropagates(t *testing.T) {
	want := errors.New("boom")
	_, err := Load(context.Background(), LoadOptions{
		ConfigMaps: []string{"x"},
		K8s:        &stubConfigMapReader{err: want},
	})
	if err == nil || !errors.Is(err, want) {
		t.Errorf("expected wrapped error, got %v", err)
	}
}

func TestInitialiseAddsNamePrefixHyphen(t *testing.T) {
	c := New()
	c.Application.NamePrefix = "tenant"
	if err := Initialise(c); err != nil {
		t.Fatalf("Initialise: %v", err)
	}
	if c.Application.NamePrefix != "tenant-" {
		t.Errorf("namePrefix not hyphen-suffixed: %q", c.Application.NamePrefix)
	}
	if c.Application.NamePrefixForEnvVars != "TENANT_" {
		t.Errorf("namePrefixForEnvVars: %q", c.Application.NamePrefixForEnvVars)
	}
}

func TestInitialiseInternalRegistry(t *testing.T) {
	c := New()
	c.Registry.Active = true
	if err := Initialise(c); err != nil {
		t.Fatalf("Initialise: %v", err)
	}
	if !c.Registry.Internal {
		t.Errorf("registry should be internal when active and no url")
	}
	want := "localhost:30000"
	if c.Registry.URL != want {
		t.Errorf("internal registry url: got %q, want %q", c.Registry.URL, want)
	}
}

func TestInitialiseRejectsImagePullSecretsWithoutCreds(t *testing.T) {
	c := New()
	c.Registry.CreateImagePullSecrets = true
	err := Initialise(c)
	if err == nil || !strings.Contains(err.Error(), "createImagePullSecrets") {
		t.Errorf("expected createImagePullSecrets error, got %v", err)
	}
}

func TestInitialiseBaseURLFanOut(t *testing.T) {
	c := New()
	c.Application.BaseURL = "http://localhost:8080"
	c.Features.ArgoCD.Active = true
	c.Features.Monitoring.Active = true
	c.Features.Secrets.Vault.Mode = "dev"
	if err := Initialise(c); err != nil {
		t.Fatalf("Initialise: %v", err)
	}
	if got, want := c.Features.ArgoCD.URL, "http://argocd.localhost:8080"; got != want {
		t.Errorf("argocd url: got %q, want %q", got, want)
	}
	if got, want := c.Features.Monitoring.GrafanaURL, "http://grafana.localhost:8080"; got != want {
		t.Errorf("grafana url: got %q, want %q", got, want)
	}
	if got, want := c.Features.Secrets.Vault.URL, "http://vault.localhost:8080"; got != want {
		t.Errorf("vault url: got %q, want %q", got, want)
	}
}

func TestInitialiseBaseURLHyphen(t *testing.T) {
	c := New()
	c.Application.BaseURL = "http://example.com"
	c.Application.URLSeparatorHyphen = true
	c.Features.ArgoCD.Active = true
	if err := Initialise(c); err != nil {
		t.Fatalf("Initialise: %v", err)
	}
	if got, want := c.Features.ArgoCD.URL, "http://argocd-example.com"; got != want {
		t.Errorf("argocd url with hyphen: got %q, want %q", got, want)
	}
}

func TestInitialiseRequiresPrefixForMultiTenant(t *testing.T) {
	c := New()
	c.MultiTenant.Raw = map[string]any{"useDedicatedInstance": true}
	err := Initialise(c)
	if err == nil || !strings.Contains(err.Error(), "Multi-Tenant") {
		t.Errorf("expected multi-tenant prefix error, got %v", err)
	}
}

func TestToYAMLRoundtrip(t *testing.T) {
	c := New()
	c.Features.ArgoCD.Active = true
	out, err := c.ToYAML(true)
	if err != nil {
		t.Fatalf("ToYAML: %v", err)
	}
	if !strings.Contains(out, "argocd:") {
		t.Errorf("expected argocd section in yaml output, got:\n%s", out)
	}
}
