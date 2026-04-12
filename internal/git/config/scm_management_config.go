package config

import "github.com/DerDaehne/gitops-playground/internal/credentials"

type GitlabManagementConfig struct {
	Url               string
	Credentials       credentials.Credentials
	ParentGroupID     string
	GitOpsUsername    string
	DefaultVisibility string
}

type SCMManagerManagementConfig struct {
	Internal       bool
	Url            string
	Credentials    credentials.Credentials
	RootPath       string
	Namespace      string
	GitOpsUsername string
}
