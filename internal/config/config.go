package config

import (
	"helm.sh/helm/v3/pkg/chartutil"
	"github.com/DerDaehne/gitops-playground/internal/features/git/config"
	"github.com/DerDaehne/gitops-playground/internal/credentials"
	"github.com/DerDaehne/gitops-playground/internal/helm"
)

// Config represents the global configuration of the application
type Config struct {
	DefaultAdminCredentials	 credentials.Credentials
	Registry				 RegistryConfig
	Jenkins					 JenkinsConfig
	MultiTenant				 MultiTenantConfig
	SCMTenant				 feature.SCMTenantConfig
	Application				 ApplicationConfig
	Features				 FeatureConfig
	Content					 ContentLoaderConfig
}

// ContentLoaderConfig represents the user-provided applications to be deployed
type ContentLoaderConfig struct {
	Examples				 bool
	MultitenancyExamples	 bool
	namespaces				 []string
	Repos					 []ContentLoaderRepo
	Variables				 map[string]string
	UseWhitelist			 bool
}

// ContentLoaderRepos represents a single repository of an app to be deployed
type ContentLoaderRepo struct {
	Url					 string
	Path				 string
	Ref					 string
	TargetRef			 string
	Credentials			 credentials.Credentials
	Templating			 bool
	Type				 ContentRepoType
	Target				 string
	OverwriteMode		 OverwriteMode
	CreateJenkinsJob	 bool
}

// RegistryConfig represents the Deployment of the docker registry
type RegistryConfig struct {
	Internal				 bool
	TwoRegistries			 bool
	Active					 bool
	InternalPort			 int
	Url						 string
	Path					 string
	Username				 string
	Password				 string
	ProxyUrl				 string
	ProxyUsername			 string
	ProxyPassword			 string
	ReadOnlyUsername		 string
	ReadOnlyPassword		 string
	CreateImagePullSecret	 bool
	Helm					 helm.HelmConfig
}

// JenkinsConfig represents the deployment of the Jenkins build server
type JenkinsConfig struct {
	Internal			 bool
	Active				 bool
	SkipRestart			 bool
	SkipPlugins			 bool
	Url					 string
	Username			 string
	Password			 string
	MetricsUsername		 string
	MetricsPassword		 string
	MavenCentralMirror	 string
	AdditionalEnvVars	 map[string]string
	Helm				 helm.HelmConfig
}

// ApplicationConfig represents the configuration for gitops-playground itself
type ApplicationConfig struct {
	RunningInsideK8s			 bool
	NamePrefixForEnvVars		 string
	InternalKubernetesApiUrl	 string
	LocalHelmChartFolder		 string
	Namespaces					 NamespaceConfig
	Debug						 bool
	Trace						 bool
	Remote						 bool
	Openshift					 bool
	Insecure					 bool
	NamePrefix					 string
	Destroy						 bool
	PodResources				 bool
	GitName						 string
	GitEmail					 string
	BaseUrl						 string
	UrlSeperatorHyphen			 bool
	MirrorRepos					 bool
	SkipCrds					 bool
	NamespaceIsolation			 bool
	Netpols						 bool
	ClusterAdmin				 bool
	Profile						 string 
}

// FeatureConfig represents all possible features to install by gitops-playground
type FeatureConfig struct {
	ArgoCD		 ArgoCDConfig
	Mail		 MailConfig
	Monitoring	 MonitoringConfig
	Secrets		 SecretsConfig
	Ingress		 IngressConfig
	CertManager	 CertManagerConfig
}

// ArgoCDConfig represents the configuration for the argocd deployment
type ArgoCDConfig struct {
	ConfigOnly					 bool
	Active						 bool
	Operator					 bool
	Url							 string
	Env							 map[string]string
	MailFrom					 string
	MailTo						 string
	MailToAdmin					 string
	ResourceInclusionsCluster	 string
	Namespace					 string
	Values						 chartutil.Values
}

// MailConfig represents the configuration for the mail-feature (deployment of a dedicated mail server or config of external smtp host)
type MailConfig struct {
	Active			 bool
	MailServer		 bool
	MailServerUrl	 string
	SmtpAddress		 string
	SmtpPort		 int
	SmptCredentials	 credentials.Credentials
	Helm			 helm.HelmConfig
}

// Monitoring represents the configuration for the monitoring feature (prometheus etc.)
type MonitoringConfig struct {
	Active							 bool
	GrafanaUrl						 string
	GrafanaMailFrom					 string
	GrafanaMailTo					 string
	GrafanaImage					 string
	GrafanaSidecarImage				 string
	PrometheusImage					 string
	PrometheusOperatorImage			 string
	PrometheusConfigReloaderImage	 string
	Helm							 helm.HelmConfig
}

// SecretsConfig represents the configuration of the secrets feature
type SecretsConfig struct {
	Active			 bool
	ExternalSecrets	 ExternalSecretsOperatorConfig
	Vault			 VaultConfig
}

// ExternalSecretsOperator represents the configuration for the external secrets k8s operator
type ExternalSecretsOperatorConfig struct {
	Image				 string
	CertControllerImage	 string
	WebHookImage		 string
	Helm				 helm.HelmConfig
}

// VaultConfig represents the Hashicorp Vault configuration
type VaultConfig struct {
	Mode	 VaultMode
	Url		 string
	Image	 string
	Helm	 helm.HelmConfig
}

// IngressConfig represents the configuration of the ingress controller
type IngressConfig struct {
	Active				 bool
	IngressNamespace	 string
	Image				 string
	Helm				 helm.HelmConfig
}

// CertManagerConfig represents the configuration of the cert-manager
type CertManagerConfig struct {
	Active bool
	Image string
	WebHookImage string
	CAInjectorImage string
	ACMESolverImage string
	StartUpAPICheckImage string
	Helm helm.HelmConfig
}

// NamespaceConfig represents all kubernetes namespaces managed by gitops-playground
type NamespaceConfig struct {
	DedicatedNamespaces	 []string
	TenantNamespaces	 []string
}


type OverwriteMode string
type ContentRepoType string
type VaultMode string

const (
	FolderBased	 ContentRepoType = "folder-based"
	Copy		 ContentRepoType = "copy"
	Mirror		 ContentRepoType = "mirror"
)

const (
	Init	 OverwriteMode = "init"
	Reset	 OverwriteMode = "reset"
	Upgrade	 OverwriteMode = "upgrade"
)

const (
	dev		 VaultMode = "dev"
	prod	 VaultMode = "prod"
)
