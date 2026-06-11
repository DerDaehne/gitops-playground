package deployment

import (
	"context"
	"fmt"

	"github.com/cloudogu/gitops-playground/go/internal/helm"
)

// HelmStrategy installs the chart imperatively via the helm CLI.
// Counterpart of infrastructure/deployment/HelmStrategy.groovy.
type HelmStrategy struct {
	Client *helm.Client
}

// Deploy implements Strategy.
func (h HelmStrategy) Deploy(ctx context.Context, s Spec) error {
	if h.Client == nil {
		return fmt.Errorf("helm strategy: client is nil")
	}
	if s.RepoType == RepoGit {
		return fmt.Errorf("helm strategy: cannot deploy chart from git URL %q via helm CLI", s.RepoURL)
	}
	if err := h.Client.AddRepo(ctx, s.RepoName, s.RepoURL); err != nil {
		return fmt.Errorf("helm add repo %q: %w", s.RepoName, err)
	}
	chart := s.RepoName + "/" + s.ChartOrPath
	opts := helm.UpgradeOptions{
		Namespace: s.Namespace,
		Version:   s.Version,
		Values:    []string{s.HelmValuesPath},
		CreateNS:  true,
	}
	if err := h.Client.Upgrade(ctx, s.ReleaseName, chart, opts); err != nil {
		return fmt.Errorf("helm upgrade %q: %w", s.ReleaseName, err)
	}
	return nil
}
