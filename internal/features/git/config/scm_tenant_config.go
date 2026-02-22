package feature

import (
	"github.com/DerDaehne/gitops-playground/internal/credentials"
	"github.com/DerDaehne/gitops-playground/internal/helm"
)


type SCMTenantConfig struct {
	SCMProvider	 SCMProviderType
	Gitlab		 GitlabTenantConfig
	SCMManager	 SCMManagerTenantConfig
}

type GitlabTenantConfig struct {
	Internal			 bool
	Url					 string
	Credentials			 credentials.Credentials
	ParentGroudID		 string
	GitOpsUsername		 string
	DefaultVisibility	 string
}

type SCMManagerTenantConfig struct {
	Internal		 bool
	Url				 string
	Namespace		 string
	Credentials		 credentials.Credentials
	RootPath		 string
	UrlForJenkins	 string
	Ingress			 string
	SkipRestart		 bool
	SkipPlugins		 bool
	GitOpsUsername	 string
	Helm			 helm.HelmConfig
}

func (s *SCMTenantConfig) Internal() bool {
	return s.Gitlab.Internal || s.SCMManager.Internal
}

type SCMProviderType string

const (
	GITLAB		 SCMProviderType = "gitlab"
	SCM_MANAGER	 SCMProviderType = "scmmanager"
)
