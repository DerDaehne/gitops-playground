package config

import "net/url"
import "helm.sh/helm/v3/pkg/chartutil"

// Config represents the global configuration of the application
type Config struct {
	Registry RegistryConfig
	Content ContentLoaderConfig
}

// ContentLoaderConfig represents the user-provided applications to be deployed
type ContentLoaderConfig struct {
	Examples bool
	MultitenancyExamples bool
	namespaces []string
	Repos []ContentLoaderRepo
	Variables map[string]string
	UseWhitelist bool
}

// ContentLoaderRepos represents a single repository of an app to be deployed
type ContentLoaderRepo struct {
	Url url.URL
	Path string
	Ref string
	TargetRef string
	Credentials Credentials
	Templating bool
	Type ContentRepoType
	Target string
	OverwriteMode OverwriteMode
	CreateJenkinsJob bool
}

// RegistryConfig represents the Deployment of the docker registry
type RegistryConfig struct {
	Internal bool
	TwoRegistries bool
	Active bool
	InternalPort int
	Url url.URL
	Path string
	Username string
	Password string
	ProxyUrl url.URL
	ProxyUsername string
	ProxyPassword string
	ReadOnlyUsername string
	ReadOnlyPassword string
	CreateImagePullSecret bool
	Helm HelmConfig
}

// JenkinsConfig represents the deployment of the Jenkins build server
type JenkinsConfig struct {
	Internal bool
	Active bool
	SkipRestart bool
	SkipPlugins bool
	Url url.URL
	Username string
	Password string
	MetricsUsername string
	MetricsPassword string
	MavenCentralMirror string
	AdditionalEnvVars map[string]string
	Helm HelmConfig
}

// ApplicationConfig represents the configuration for gitops-playground itself
type ApplicationConfig struct {
	RunningInsideK8s bool
	NamePrefixForEnvVars string
	InternalKubernetesApiUrl url.URL
	LocalHelmChartFolder string
	Namespaces NamespaceConfig
	Debug bool
	Trace bool
	Remote bool
	Openshift bool
	Insecure bool
	NamePrefix string
	Destroy bool
	PodResources bool
	GitName string
	GitEmail string
	BaseUrl url.URL
	UrlSeperatorHyphen bool
	MirrorRepos bool
	SkipCrds bool
	NamespaceIsolation bool
	Netpols bool
	ClusterAdmin bool
	Profile string 
}

// FeatureConfig represents all possible features to install by gitops-playground
type FeatureConfig struct {
	ArgoCD ArgoCDConfig
	Mail MailConfig
	Monitoring MonitoringConfig
	Secrets SecretsConfig
	Ingress IngressConfig
	CertManager CertManagerConfig
}

// ArgoCDConfig represents the configuration for the argocd deployment
type ArgoCDConfig struct {
	ConfigOnly bool
	Active bool
	Operator bool
	Url url.URL
	Env map[string]string
	MailFrom string
	MailTo string
	MailToAdmin string
	ResourceInclusionsCluster string
	Namespace string
	Values chartutil.Values
}

// NamespaceConfig represents all kubernetes namespaces managed by gitops-playground
type NamespaceConfig struct {
	DedicatedNamespaces []string
	TenantNamespaces []string
}

// HelmConfig represents all data needed to run an helm deployment
type HelmConfig struct {
	Chart string
	RepoUrl string
	Version string
	Values chartutil.Values
}

// Credentials represents a username/password combination
type Credentials struct {
	Username string
	Passwort string
}


type OverwriteMode int
type ContentRepoType int

const (
	FolderBased ContentRepoType = iota
	Copy
	Mirror
)

const (
	Init OverwriteMode = iota
	Reset
	Upgrade
)
