package git

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/DerDaehne/gitops-playground/internal/config"
	gitconfig "github.com/DerDaehne/gitops-playground/internal/git/config"
	"github.com/DerDaehne/gitops-playground/internal/git/gitlab"
	"github.com/DerDaehne/gitops-playground/internal/git/provider"
	"github.com/DerDaehne/gitops-playground/internal/git/scmmanager"
)

type GitHandler struct {
	Config *config.Config
	Tenant provider.GitProvider
}

func (g *GitHandler) Name() string {
	return "GitHandler"
}

func (g *GitHandler) IsEnabled() bool {
	return true
}

func (g *GitHandler) Validate() error {
	if g.Config.SCM.SCMManager.Url != "" {
		g.Config.SCM.SCMManager.Internal = false
		g.Config.SCM.SCMManager.UrlForJenkins = g.Config.SCM.SCMManager.Url
	} else {
		slog.Debug("Setting configs for internal SCM-Manager")
		g.Config.SCM.SCMManager.UrlForJenkins = fmt.Sprintf(
			"http://scmm.%sscm-manager.svc.cluster.local/scm",
			g.Config.Application.NamePrefix,
		)
	}
	g.Config.SCM.SCMManager.GitOpsUsername = g.Config.Application.NamePrefix + "gitops"

	if g.Config.SCM.Gitlab.Url != "" {
		g.Config.SCM.SCMProvider = gitconfig.GITLAB
		g.Config.SCM.SCMManager = gitconfig.SCMManagerTenantConfig{}
		if g.Config.SCM.Gitlab.Credentials.Password == "" || g.Config.SCM.Gitlab.ParentGroudID == "" {
			return fmt.Errorf("GitLab configuration incomplete: please provide both password (PAT) and parentGroupId")
		}
	}

	return nil
}

func (g *GitHandler) Install() error {
	switch g.Config.SCM.SCMProvider {
	case gitconfig.GITLAB:
		g.Tenant = gitlab.NewGitlab(&g.Config.SCM.Gitlab)
	case gitconfig.SCM_MANAGER, "":
		prefixedNamespace := g.Config.Application.NamePrefix + "scm-manager"
		g.Config.SCM.SCMManager.Namespace = prefixedNamespace
		sm, err := scmmanager.NewScmManager(g.Config, &g.Config.SCM.SCMManager, true)
		if err != nil {
			return err
		}
		g.Tenant = sm
	default:
		return fmt.Errorf("unsupported SCM provider: %s", g.Config.SCM.SCMProvider)
	}

	namePrefix := strings.TrimSpace(g.Config.Application.NamePrefix)
	return setupRepos(g.Tenant, namePrefix)
}

func setupRepos(gitProvider provider.GitProvider, namePrefix string) error {
	return gitProvider.CreateRepository(
		WithOrgPrefix(namePrefix, "argocd/cluster-resources"),
		"GitOps repo for basic cluster-resources",
	)
}

func WithOrgPrefix(prefix string, repoPath string) string {
	if prefix == "" {
		return repoPath
	}
	return prefix + repoPath
}
