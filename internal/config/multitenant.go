package config

import (
	feature "github.com/DerDaehne/gitops-playground/internal/git/config"
)

type MultiTenantConfig struct {
	SCMProvider feature.SCMProviderType
	Gitlab feature.GitlabManagementConfig
	SCMManager feature.SCMManagerManagementConfig
	CentralArgoCDNamespace string
	UseDedicatedInstance bool

}
