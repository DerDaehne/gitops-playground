package config

import (
	"github.com/DerDaehne/gitops-playground/internal/features/git/config"
)

type MultiTenantConfig struct {
	SCMProvider feature.SCMProviderType
	Gitlab feature.GitlabManagementConfig
	SCMManager feature.SCMManagerManagementConfig
	CentralArgoCDNamespace string
	UseDedicatedInstance bool

}
