package config

// This file mirrors the nested schema classes from
// com.cloudogu.gitops.config.Config. Field names follow Go convention,
// while YAML tags reproduce the original Groovy property names so existing
// config files stay compatible.

// HelmConfig matches Config.HelmConfig.
type HelmConfig struct {
	Chart   string `yaml:"chart,omitempty"`
	RepoURL string `yaml:"repoURL,omitempty"`
	Version string `yaml:"version,omitempty"`
}

// HelmConfigWithValues matches Config.HelmConfigWithValues.
type HelmConfigWithValues struct {
	HelmConfig `yaml:",inline"`
	Values     map[string]any `yaml:"values,omitempty"`
}

// RegistrySchema matches Config.RegistrySchema.
type RegistrySchema struct {
	Internal               bool                 `yaml:"internal"`
	TwoRegistries          bool                 `yaml:"twoRegistries"`
	Active                 bool                 `yaml:"active"`
	InternalPort           int                  `yaml:"internalPort"`
	URL                    string               `yaml:"url"`
	Path                   string               `yaml:"path"`
	Username               string               `yaml:"username"`
	Password               string               `yaml:"password"`
	ProxyURL               string               `yaml:"proxyUrl"`
	ProxyPath              string               `yaml:"proxyPath"`
	ProxyUsername          string               `yaml:"proxyUsername"`
	ProxyPassword          string               `yaml:"proxyPassword"`
	ReadOnlyUsername       string               `yaml:"readOnlyUsername"`
	ReadOnlyPassword       string               `yaml:"readOnlyPassword"`
	CreateImagePullSecrets bool                 `yaml:"createImagePullSecrets"`
	Helm                   HelmConfigWithValues `yaml:"helm"`
}

// JenkinsSchema matches Config.JenkinsSchema.
type JenkinsSchema struct {
	Internal                    bool                 `yaml:"internal"`
	URLForScm                   string               `yaml:"urlForScm"`
	Ingress                     string               `yaml:"ingress"`
	InternalBashImage           string               `yaml:"internalBashImage"`
	InternalDockerClientVersion string               `yaml:"internalDockerClientVersion"`
	Active                      bool                 `yaml:"active"`
	SkipRestart                 bool                 `yaml:"skipRestart"`
	SkipPlugins                 bool                 `yaml:"skipPlugins"`
	URL                         string               `yaml:"url"`
	Username                    string               `yaml:"username"`
	Password                    string               `yaml:"password"`
	MetricsUsername             string               `yaml:"metricsUsername"`
	MetricsPassword             string               `yaml:"metricsPassword"`
	MavenCentralMirror          string               `yaml:"mavenCentralMirror"`
	AdditionalEnvs              map[string]string    `yaml:"additionalEnvs"`
	Helm                        HelmConfigWithValues `yaml:"helm"`
}

// MultiTenantSchema matches Config.MultiTenantSchema (kept opaque for now).
type MultiTenantSchema struct {
	// Concrete fields will be added in Phase 2 alongside the SCM split.
	Raw map[string]any `yaml:",inline"`
}

// ScmSchema matches Config.ScmTenantSchema (kept opaque for now; Phase 2
// fills in the specifics, see ScmTenantSchema.groovy + ScmCentralSchema).
type ScmSchema struct {
	Raw map[string]any `yaml:",inline"`
}

// ApplicationSchema matches Config.ApplicationSchema.
type ApplicationSchema struct {
	RunningInsideK8s        bool             `yaml:"runningInsideK8s"`
	NamePrefixForEnvVars    string           `yaml:"namePrefixForEnvVars"`
	InternalKubernetesAPIURL string          `yaml:"internalKubernetesApiUrl"`
	LocalHelmChartFolder    string           `yaml:"localHelmChartFolder,omitempty"`
	Namespaces              NamespaceSchema  `yaml:"namespaces"`
	ConfigFiles             []string         `yaml:"configFiles,omitempty"`
	ConfigMaps              []string         `yaml:"configMaps,omitempty"`
	Debug                   bool             `yaml:"-"`
	Trace                   bool             `yaml:"-"`
	OutputConfigFile        bool             `yaml:"-"`
	Insecure                bool             `yaml:"insecure"`
	Openshift               bool             `yaml:"openshift"`
	Username                string           `yaml:"username"`
	Password                string           `yaml:"password"`
	Yes                     bool             `yaml:"-"`
	NamePrefix              string           `yaml:"namePrefix"`
	Destroy                 bool             `yaml:"-"`
	PodResources            bool             `yaml:"podResources"`
	GitName                 string           `yaml:"gitName"`
	GitEmail                string           `yaml:"gitEmail"`
	BaseURL                 string           `yaml:"baseUrl"`
	URLSeparatorHyphen      bool             `yaml:"urlSeparatorHyphen"`
	MirrorRepos             bool             `yaml:"mirrorRepos"`
	SkipCRDs                bool             `yaml:"skipCrds"`
	NamespaceIsolation      bool             `yaml:"namespaceIsolation"`
	NetPols                 bool             `yaml:"netpols"`
	ClusterAdmin            bool             `yaml:"clusterAdmin"`
	Profile                 string           `yaml:"profile,omitempty"`
	GopNamespace            string           `yaml:"gopNamespace"`
}

// TenantName returns the namePrefix without trailing hyphen.
func (a ApplicationSchema) TenantName() string {
	if n := a.NamePrefix; n != "" && n[len(n)-1] == '-' {
		return n[:len(n)-1]
	}
	return a.NamePrefix
}

// NamespaceSchema matches ApplicationSchema.NamespaceSchema.
type NamespaceSchema struct {
	DedicatedNamespaces []string `yaml:"dedicatedNamespaces"`
	TenantNamespaces    []string `yaml:"tenantNamespaces"`
}

// Active returns the union of dedicated and tenant namespaces, preserving
// insertion order without duplicates.
func (n NamespaceSchema) Active() []string {
	seen := make(map[string]struct{}, len(n.DedicatedNamespaces)+len(n.TenantNamespaces))
	out := make([]string, 0, len(n.DedicatedNamespaces)+len(n.TenantNamespaces))
	for _, ns := range append(append([]string(nil), n.DedicatedNamespaces...), n.TenantNamespaces...) {
		if _, ok := seen[ns]; ok {
			continue
		}
		seen[ns] = struct{}{}
		out = append(out, ns)
	}
	return out
}

// FeaturesSchema matches Config.FeaturesSchema.
type FeaturesSchema struct {
	ArgoCD      ArgoCDSchema      `yaml:"argocd"`
	Mail        MailSchema        `yaml:"mail"`
	Monitoring  MonitoringSchema  `yaml:"monitoring"`
	Secrets     SecretsSchema     `yaml:"secrets"`
	Ingress     IngressSchema     `yaml:"ingress"`
	CertManager CertManagerSchema `yaml:"certManager"`
}

// ArgoCDSchema matches Config.ArgoCDSchema.
type ArgoCDSchema struct {
	ConfigOnly                bool                `yaml:"configOnly"`
	Active                    bool                `yaml:"active"`
	Operator                  bool                `yaml:"operator"`
	URL                       string              `yaml:"url"`
	Env                       []map[string]string `yaml:"env,omitempty"`
	EmailFrom                 string              `yaml:"emailFrom"`
	EmailToUser               string              `yaml:"emailToUser"`
	EmailToAdmin              string              `yaml:"emailToAdmin"`
	ResourceInclusionsCluster string              `yaml:"resourceInclusionsCluster"`
	Namespace                 string              `yaml:"namespace"`
	Values                    map[string]any      `yaml:"values,omitempty"`
}

// MailSchema matches Config.MailSchema.
type MailSchema struct {
	Active       bool   `yaml:"active"`
	SMTPAddress  string `yaml:"smtpAddress"`
	SMTPPort     int    `yaml:"smtpPort,omitempty"`
	SMTPUser     string `yaml:"smtpUser"`
	SMTPPassword string `yaml:"smtpPassword"`
}

// MonitoringSchema matches Config.MonitoringSchema.
type MonitoringSchema struct {
	Active           bool                 `yaml:"active"`
	GrafanaURL       string               `yaml:"grafanaUrl"`
	GrafanaEmailFrom string               `yaml:"grafanaEmailFrom"`
	GrafanaEmailTo   string               `yaml:"grafanaEmailTo"`
	Helm             MonitoringHelmSchema `yaml:"helm"`
}

// MonitoringHelmSchema matches Config.MonitoringSchema.MonitoringHelmSchema.
type MonitoringHelmSchema struct {
	HelmConfigWithValues          `yaml:",inline"`
	GrafanaImage                  string `yaml:"grafanaImage"`
	GrafanaSidecarImage           string `yaml:"grafanaSidecarImage"`
	PrometheusImage               string `yaml:"prometheusImage"`
	PrometheusOperatorImage       string `yaml:"prometheusOperatorImage"`
	PrometheusConfigReloaderImage string `yaml:"prometheusConfigReloaderImage"`
}

// SecretsSchema matches Config.SecretsSchema.
type SecretsSchema struct {
	Active          bool        `yaml:"active"`
	ExternalSecrets ESOSchema   `yaml:"externalSecrets"`
	Vault           VaultSchema `yaml:"vault"`
}

// ESOSchema matches Config.SecretsSchema.ESOSchema.
type ESOSchema struct {
	Helm ESOHelmSchema `yaml:"helm"`
}

// ESOHelmSchema matches Config.SecretsSchema.ESOSchema.ESOHelmSchema.
type ESOHelmSchema struct {
	HelmConfigWithValues `yaml:",inline"`
	Image                string `yaml:"image"`
	CertControllerImage  string `yaml:"certControllerImage"`
	WebhookImage         string `yaml:"webhookImage"`
}

// VaultSchema matches Config.SecretsSchema.VaultSchema.
type VaultSchema struct {
	Mode string          `yaml:"mode,omitempty"`
	URL  string          `yaml:"url"`
	Helm VaultHelmSchema `yaml:"helm"`
}

// VaultHelmSchema matches Config.SecretsSchema.VaultSchema.VaultHelmSchema.
type VaultHelmSchema struct {
	HelmConfigWithValues `yaml:",inline"`
	Image                string `yaml:"image"`
}

// IngressSchema matches Config.IngressSchema.
type IngressSchema struct {
	Active           bool              `yaml:"active"`
	Helm             IngressHelmSchema `yaml:"helm"`
	IngressNamespace string            `yaml:"ingressNamespace"`
}

// IngressHelmSchema matches Config.IngressSchema.IngressHelmSchema.
type IngressHelmSchema struct {
	HelmConfigWithValues `yaml:",inline"`
	Image                string `yaml:"image"`
}

// CertManagerSchema matches Config.CertManagerSchema.
type CertManagerSchema struct {
	Active bool                  `yaml:"active"`
	Issuer string                `yaml:"issuer"`
	Helm   CertManagerHelmSchema `yaml:"helm"`
}

// CertManagerHelmSchema matches Config.CertManagerSchema.CertManagerHelmSchema.
type CertManagerHelmSchema struct {
	HelmConfigWithValues `yaml:",inline"`
	Image                string `yaml:"image"`
	WebhookImage         string `yaml:"webhookImage"`
	CAInjectorImage      string `yaml:"cainjectorImage"`
	AcmeSolverImage      string `yaml:"acmeSolverImage"`
	StartupAPICheckImage string `yaml:"startupAPICheckImage"`
}

// ContentSchema matches Config.ContentSchema (HelmRelease/ContentRepo
// subtypes follow in Phase 2 when the ContentLoader is ported).
type ContentSchema struct {
	Namespaces              []string                 `yaml:"namespaces,omitempty"`
	Repos                   []ContentRepositorySchema `yaml:"repos,omitempty"`
	Variables               map[string]any           `yaml:"variables,omitempty"`
	HelmReleases            []HelmReleaseSchema      `yaml:"helmReleases,omitempty"`
	UseWhitelist            bool                     `yaml:"useWhitelist"`
	AllowedStaticsWhitelist []string                 `yaml:"allowedStaticsWhitelist,omitempty"`
}

// ContentRepositorySchema matches ContentSchema.ContentRepositorySchema.
type ContentRepositorySchema struct {
	URL              string         `yaml:"url"`
	Path             string         `yaml:"path,omitempty"`
	Ref              string         `yaml:"ref,omitempty"`
	TargetRef        string         `yaml:"targetRef,omitempty"`
	Credentials      *Credentials   `yaml:"credentials,omitempty"`
	Templating       bool           `yaml:"templating,omitempty"`
	Type             string         `yaml:"type,omitempty"`
	Target           string         `yaml:"target,omitempty"`
	OverwriteMode    string         `yaml:"overwriteMode,omitempty"`
	CreateJenkinsJob bool           `yaml:"createJenkinsJob,omitempty"`
}

// HelmReleaseSchema matches ContentSchema.HelmReleaseSchema.
type HelmReleaseSchema struct {
	Name        string         `yaml:"name"`
	RepoURL     string         `yaml:"repoURL"`
	Chart       string         `yaml:"chart"`
	Version     string         `yaml:"version"`
	Namespace   string         `yaml:"namespace"`
	ReleaseName string         `yaml:"releaseName,omitempty"`
	ValuesPath  string         `yaml:"valuesPath,omitempty"`
	Values      map[string]any `yaml:"values,omitempty"`
}

// Credentials mirrors Config.Credentials (kept minimal; expand in Phase 2).
type Credentials struct {
	Username    string `yaml:"username,omitempty"`
	Password    string `yaml:"password,omitempty"`
	SecretRef   string `yaml:"secretRef,omitempty"`
	SecretKey   string `yaml:"secretKey,omitempty"`
	SecretField string `yaml:"secretField,omitempty"`
}
