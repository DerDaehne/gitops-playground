package provider

import (
	"net/url"

	"github.com/DerDaehne/gitops-playground/internal/credentials"
)

type GitProvider interface {
	CreateRepository(repoTarget string, description string) error
	SetRepositoryPermission(repoTarget string, principal string, role AccessRole, scope Scope) error
	RepoUrl(repoTarget string, scope RepoUrlScope) string
	RepoPrefix() string
	GetUrl() string
	GetProtocol() string
	GetHost() string
	GetCredentials() credentials.Credentials
	GetGitOpsUsername() string
	PrometheusMetricsEndpoint() *url.URL
}

type AccessRole string

const (
	AccessRoleRead     AccessRole = "READ"
	AccessRoleWrite    AccessRole = "WRITE"
	AccessRoleMaintain AccessRole = "MAINTAIN"
	AccessRoleAdmin    AccessRole = "ADMIN"
	AccessRoleOwner    AccessRole = "OWNER"
)

type Scope string

const (
	ScopeUser  Scope = "USER"
	ScopeGroup Scope = "GROUP"
)

type RepoUrlScope string

const (
	RepoUrlScopeInCluster RepoUrlScope = "IN_CLUSTER"
	RepoUrlScopeClient    RepoUrlScope = "CLIENT"
)
