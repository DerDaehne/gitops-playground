package gitlab

import (
	"log/slog"
	"net/url"

	feature "github.com/DerDaehne/gitops-playground/internal/features/git/config"
	"github.com/DerDaehne/gitops-playground/internal/features/git/provider"
	"github.com/DerDaehne/gitops-playground/internal/credentials"
)

type Gitlab struct {
	config *feature.GitlabTenantConfig
}

func NewGitlab(config *feature.GitlabTenantConfig) *Gitlab {
	return &Gitlab{config: config}
}

func (gl *Gitlab) CreateRepository(repoTarget string, description string) error {
	slog.Warn("GitLab provider not implemented yet", "repoTarget", repoTarget)
	return nil
}

func (gl *Gitlab) SetRepositoryPermission(repoTarget string, principal string, role provider.AccessRole, scope provider.Scope) error {
	slog.Warn("GitLab provider not implemented yet")
	return nil
}

func (gl *Gitlab) RepoUrl(repoTarget string, scope provider.RepoUrlScope) string {
	slog.Warn("GitLab provider not implemented yet")
	return ""
}

func (gl *Gitlab) RepoPrefix() string {
	return ""
}

func (gl *Gitlab) GetUrl() string {
	return gl.config.Url
}

func (gl *Gitlab) GetProtocol() string {
	u, err := url.Parse(gl.config.Url)
	if err != nil {
		return "https"
	}
	return u.Scheme
}

func (gl *Gitlab) GetHost() string {
	u, err := url.Parse(gl.config.Url)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (gl *Gitlab) GetCredentials() credentials.Credentials {
	return gl.config.Credentials
}

func (gl *Gitlab) GetGitOpsUsername() string {
	return gl.config.GitOpsUsername
}

func (gl *Gitlab) PrometheusMetricsEndpoint() *url.URL {
	return nil
}
