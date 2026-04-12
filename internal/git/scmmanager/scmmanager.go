package scmmanager

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/DerDaehne/gitops-playground/internal/config"
	gitconfig "github.com/DerDaehne/gitops-playground/internal/git/config"
	"github.com/DerDaehne/gitops-playground/internal/git/provider"
	"github.com/DerDaehne/gitops-playground/internal/git/scmmanager/api"
	"github.com/DerDaehne/gitops-playground/internal/credentials"
)

type ScmManager struct {
	urls       *ScmManagerUrlResolver
	apiClient  *api.ScmManagerApiClient
	scmmConfig *gitconfig.SCMManagerTenantConfig
	appConfig  *config.ApplicationConfig
}

func NewScmManager(globalConfig *config.Config, scmmConfig *gitconfig.SCMManagerTenantConfig, installNeeded bool) (*ScmManager, error) {
	sm := &ScmManager{
		scmmConfig: scmmConfig,
		appConfig:  &globalConfig.Application,
	}

	if scmmConfig.Internal && installNeeded {
		if err := SetupHelm(scmmConfig); err != nil {
			return nil, err
		}

		sm.urls = NewScmManagerUrlResolver(&globalConfig.Application, scmmConfig)
		sm.apiClient = api.NewScmManagerApiClient(sm.urls.ClientApiBase().String(), scmmConfig.Credentials)

		if err := WaitForScmmAvailable(sm.apiClient, 180, 5000); err != nil {
			return nil, err
		}

		if err := Configure(sm.apiClient, &globalConfig.Application, scmmConfig, &globalConfig.Jenkins, sm.GetUrl()); err != nil {
			return nil, err
		}
	} else {
		sm.urls = NewScmManagerUrlResolver(&globalConfig.Application, scmmConfig)
		sm.apiClient = api.NewScmManagerApiClient(sm.urls.ClientApiBase().String(), scmmConfig.Credentials)
	}

	return sm, nil
}

func (sm *ScmManager) CreateRepository(repoTarget string, description string) error {
	parts := strings.SplitN(repoTarget, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid repoTarget format: %s (expected namespace/name)", repoTarget)
	}
	repoNamespace := parts[0]
	repoName := parts[1]

	repo := api.Repository{
		Namespace:   repoNamespace,
		Name:        repoName,
		Type:        "git",
		Description: description,
	}

	slog.Info("Creating repository " + repoTarget)
	return sm.apiClient.CreateRepository(repo, true)
}

func (sm *ScmManager) SetRepositoryPermission(repoTarget string, principal string, role provider.AccessRole, scope provider.Scope) error {
	parts := strings.SplitN(repoTarget, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid repoTarget format: %s (expected namespace/name)", repoTarget)
	}
	repoNamespace := parts[0]
	repoName := parts[1]

	scmManagerRole := mapToScmManager(role)
	isGroup := scope == provider.ScopeGroup
	perm := api.Permission{
		Name:            principal,
		Role:            scmManagerRole,
		GroupPermission: isGroup,
	}

	return sm.apiClient.CreatePermission(repoNamespace, repoName, perm)
}

func (sm *ScmManager) RepoUrl(repoTarget string, scope provider.RepoUrlScope) string {
	switch scope {
	case provider.RepoUrlScopeClient:
		return sm.urls.ClientRepoUrl(repoTarget)
	default:
		return sm.urls.InClusterRepoUrl(repoTarget)
	}
}

func (sm *ScmManager) RepoPrefix() string {
	return sm.urls.InClusterRepoPrefix()
}

func (sm *ScmManager) GetUrl() string {
	return sm.urls.InClusterBase().String()
}

func (sm *ScmManager) GetProtocol() string {
	return sm.urls.InClusterBase().Scheme
}

func (sm *ScmManager) GetHost() string {
	return sm.urls.InClusterBase().Hostname()
}

func (sm *ScmManager) GetCredentials() credentials.Credentials {
	return sm.scmmConfig.Credentials
}

func (sm *ScmManager) GetGitOpsUsername() string {
	return sm.scmmConfig.GitOpsUsername
}

func (sm *ScmManager) PrometheusMetricsEndpoint() *url.URL {
	return sm.urls.PrometheusEndpoint()
}

func mapToScmManager(role provider.AccessRole) api.PermissionRole {
	switch role {
	case provider.AccessRoleRead:
		return api.PermissionRoleRead
	case provider.AccessRoleWrite:
		return api.PermissionRoleWrite
	case provider.AccessRoleMaintain:
		slog.Warn("SCM-Manager: Mapping MAINTAIN -> WRITE")
		return api.PermissionRoleWrite
	case provider.AccessRoleAdmin:
		return api.PermissionRoleOwner
	case provider.AccessRoleOwner:
		return api.PermissionRoleOwner
	default:
		return api.PermissionRoleRead
	}
}
