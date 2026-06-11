package cli

import (
	"github.com/cloudogu/gitops-playground/go/internal/config"

	"github.com/spf13/pflag"
)

// Each register* function attaches a related cluster of flags to the
// command, matching the Picocli @Option layout in the Groovy Config.

func registerGlobalFlags(f *pflag.FlagSet, c *config.Config) {
	a := &c.Application
	f.StringSliceVar(&a.ConfigFiles, "config-file", a.ConfigFiles, "Config file for the application")
	f.StringSliceVar(&a.ConfigMaps, "config-map", a.ConfigMaps, "Kubernetes configuration map. Should contain a key `config.yaml`.")
	f.BoolVarP(&a.Debug, "debug", "d", a.Debug, "Debug output")
	f.BoolVarP(&a.Trace, "trace", "x", a.Trace, "Debug + Show each command executed (set -x)")
	f.StringVarP(&a.Profile, "profile", "p", a.Profile, "Use predefined profile (full, only-argocd, operator-mandants aso.)")
	f.BoolVarP(&a.Yes, "yes", "y", a.Yes, "Skip confirmation")
}

func registerApplicationFlags(f *pflag.FlagSet, c *config.Config) {
	a := &c.Application
	f.BoolVar(&a.Insecure, "insecure", a.Insecure, "Sets insecure-mode in cURL which skips cert validation")
	f.BoolVar(&a.Openshift, "openshift", a.Openshift, "When set, openshift specific resources and configurations are applied")
	f.StringVar(&a.Username, "username", a.Username, "Set initial admin username")
	f.StringVar(&a.Password, "password", a.Password, "Set initial admin passwords")
	f.StringVar(&a.NamePrefix, "name-prefix", a.NamePrefix, "Set name-prefix for repos, jobs, namespaces")
	f.BoolVar(&a.Destroy, "destroy", a.Destroy, "Unroll playground")
	f.BoolVar(&a.PodResources, "pod-resources", a.PodResources, "Write kubernetes resource requests and limits on each pod")
	f.StringVar(&a.GitName, "git-name", a.GitName, "Sets git author and committer name used for initial commits")
	f.StringVar(&a.GitEmail, "git-email", a.GitEmail, "Sets git author and committer email used for initial commits")
	f.StringVar(&a.BaseURL, "base-url", a.BaseURL, "the external base url (TLD) for all tools")
	f.BoolVar(&a.URLSeparatorHyphen, "url-separator-hyphen", a.URLSeparatorHyphen, "Use hyphens instead of dots to separate application name from base-url")
	f.BoolVar(&a.MirrorRepos, "mirror-repos", a.MirrorRepos, "Pull tool sources from git instead of the internet (air-gapped).")
	f.BoolVar(&a.SkipCRDs, "skip-crds", a.SkipCRDs, "Skip installation of CRDs. Requires CRDs already installed.")
	f.BoolVar(&a.NamespaceIsolation, "namespace-isolation", a.NamespaceIsolation, "Configure tools to work with the given namespaces only.")
	f.BoolVar(&a.NetPols, "netpols", a.NetPols, "Sets Network Policies")
	f.BoolVar(&a.ClusterAdmin, "cluster-admin", a.ClusterAdmin, "Binds ArgoCD controllers to cluster-admin ClusterRole")
	f.StringVar(&a.GopNamespace, "gop-namespace", a.GopNamespace, "If set, GOP stores specific information in this namespace.")
}

func registerRegistryFlags(f *pflag.FlagSet, c *config.Config) {
	r := &c.Registry
	f.BoolVar(&r.Active, "registry", r.Active, "Installs a simple cluster-local registry for demonstration purposes.")
	f.IntVar(&r.InternalPort, "internal-registry-port", r.InternalPort, "Port of registry. Ignored when a registry*url params are set")
	f.StringVar(&r.URL, "registry-url", r.URL, "The url of your external registry, used for pushing images")
	f.StringVar(&r.Path, "registry-path", r.Path, "Optional when registry-url is set")
	f.StringVar(&r.Username, "registry-username", r.Username, "Optional when registry-url is set")
	f.StringVar(&r.Password, "registry-password", r.Password, "Optional when registry-url is set")
	f.StringVar(&r.ProxyURL, "registry-proxy-url", r.ProxyURL, "The url of your proxy-registry.")
	f.StringVar(&r.ProxyPath, "registry-proxy-path", r.ProxyPath, "Optional when registry-proxy-url is set")
	f.StringVar(&r.ProxyUsername, "registry-proxy-username", r.ProxyUsername, "Username for proxy registry")
	f.StringVar(&r.ProxyPassword, "registry-proxy-password", r.ProxyPassword, "Password for proxy registry")
	f.StringVar(&r.ReadOnlyUsername, "registry-username-read-only", r.ReadOnlyUsername, "Read-only username for registry-url")
	f.StringVar(&r.ReadOnlyPassword, "registry-password-read-only", r.ReadOnlyPassword, "Read-only password for registry-url")
	f.BoolVar(&r.CreateImagePullSecrets, "create-image-pull-secrets", r.CreateImagePullSecrets, "Create image pull secrets for registry and proxy-registry")
}

func registerJenkinsFlags(f *pflag.FlagSet, c *config.Config) {
	j := &c.Jenkins
	f.BoolVar(&j.Active, "jenkins", j.Active, "Installs Jenkins as CI server")
	f.BoolVar(&j.SkipRestart, "jenkins-skip-restart", j.SkipRestart, "Skips restarting Jenkins after plugin installation.")
	f.BoolVar(&j.SkipPlugins, "jenkins-skip-plugins", j.SkipPlugins, "Skips plugin installation.")
	f.StringVar(&j.URL, "jenkins-url", j.URL, "The url of your external jenkins")
	f.StringVar(&j.Username, "jenkins-username", j.Username, "Mandatory when jenkins-url is set")
	f.StringVar(&j.Password, "jenkins-password", j.Password, "Mandatory when jenkins-url is set")
	f.StringVar(&j.MetricsUsername, "jenkins-metrics-username", j.MetricsUsername, "Metrics scrape user for Jenkins")
	f.StringVar(&j.MetricsPassword, "jenkins-metrics-password", j.MetricsPassword, "Metrics scrape password for Jenkins")
	f.StringVar(&j.MavenCentralMirror, "maven-central-mirror", j.MavenCentralMirror, "URL for maven mirror used in pipelines")
	f.StringToStringVar(&j.AdditionalEnvs, "jenkins-additional-envs", j.AdditionalEnvs, "Set additional environments to Jenkins")
}

func registerArgoCDFlags(f *pflag.FlagSet, c *config.Config) {
	a := &c.Features.ArgoCD
	f.BoolVar(&a.Active, "argocd", a.Active, "Install ArgoCD")
	f.BoolVar(&a.Operator, "argocd-operator", a.Operator, "Install ArgoCD via an already running ArgoCD Operator")
	f.StringVar(&a.URL, "argocd-url", a.URL, "The URL where argocd is accessible.")
	f.StringVar(&a.EmailFrom, "argocd-email-from", a.EmailFrom, "Argo CD sender email address")
	f.StringVar(&a.EmailToUser, "argocd-email-to-user", a.EmailToUser, "Argo CD user / app-team recipient email")
	f.StringVar(&a.EmailToAdmin, "argocd-email-to-admin", a.EmailToAdmin, "Argo CD admin recipient email")
	f.StringVar(&a.ResourceInclusionsCluster, "argocd-resource-inclusions-cluster", a.ResourceInclusionsCluster, "Internal Kubernetes API Server URL for argocd-operator resourceInclusions")
	f.StringVar(&a.Namespace, "argocd-namespace", a.Namespace, "Defines the kubernetes namespace for ArgoCD")
}

func registerMonitoringFlags(f *pflag.FlagSet, c *config.Config) {
	m := &c.Features.Monitoring
	f.BoolVar(&m.Active, "monitoring", m.Active, "Installs the Kube-Prometheus-Stack")
	// alias --metrics
	f.BoolVar(&m.Active, "metrics", m.Active, "Alias for --monitoring")
	f.StringVar(&m.GrafanaURL, "grafana-url", m.GrafanaURL, "Sets url for grafana")
	f.StringVar(&m.GrafanaEmailFrom, "grafana-email-from", m.GrafanaEmailFrom, "Grafana alerts sender email")
	f.StringVar(&m.GrafanaEmailTo, "grafana-email-to", m.GrafanaEmailTo, "Grafana alerts recipient email")
	f.StringVar(&m.Helm.GrafanaImage, "grafana-image", m.Helm.GrafanaImage, "Sets image for grafana")
	f.StringVar(&m.Helm.GrafanaSidecarImage, "grafana-sidecar-image", m.Helm.GrafanaSidecarImage, "Sets image for grafana sidecar")
	f.StringVar(&m.Helm.PrometheusImage, "prometheus-image", m.Helm.PrometheusImage, "Sets image for prometheus")
	f.StringVar(&m.Helm.PrometheusOperatorImage, "prometheus-operator-image", m.Helm.PrometheusOperatorImage, "Sets image for prometheus-operator")
	f.StringVar(&m.Helm.PrometheusConfigReloaderImage, "prometheus-config-reloader-image", m.Helm.PrometheusConfigReloaderImage, "Sets image for prometheus-operator's config-reloader")
}

func registerSecretsFlags(f *pflag.FlagSet, c *config.Config) {
	v := &c.Features.Secrets.Vault
	eso := &c.Features.Secrets.ExternalSecrets

	f.StringVar(&v.Mode, "vault", v.Mode, "Installs Hashicorp vault. Possible values: dev, prod.")
	f.StringVar(&v.URL, "vault-url", v.URL, "Sets url for vault ui")
	f.StringVar(&v.Helm.Image, "vault-image", v.Helm.Image, "Sets image for vault")

	f.StringVar(&eso.Helm.Image, "external-secrets-image", eso.Helm.Image, "Sets image for external secrets operator")
	f.StringVar(&eso.Helm.CertControllerImage, "external-secrets-certcontroller-image", eso.Helm.CertControllerImage, "Sets cert-controller image for ESO")
	f.StringVar(&eso.Helm.WebhookImage, "external-secrets-webhook-image", eso.Helm.WebhookImage, "Sets webhook image for ESO")
}

func registerMailFlags(f *pflag.FlagSet, c *config.Config) {
	m := &c.Features.Mail
	f.StringVar(&m.SMTPAddress, "smtp-address", m.SMTPAddress, "Sets smtp address of external Mailserver")
	f.IntVar(&m.SMTPPort, "smtp-port", m.SMTPPort, "Sets smtp port of external Mailserver")
	f.StringVar(&m.SMTPUser, "smtp-user", m.SMTPUser, "Sets smtp username")
	f.StringVar(&m.SMTPPassword, "smtp-password", m.SMTPPassword, "Sets smtp password")
}

func registerIngressFlags(f *pflag.FlagSet, c *config.Config) {
	i := &c.Features.Ingress
	f.BoolVar(&i.Active, "ingress", i.Active, "Sets and enables Ingress Controller")
	f.StringVar(&i.Helm.Image, "ingress-image", i.Helm.Image, "Image for the ingress controller")
}

func registerCertManagerFlags(f *pflag.FlagSet, c *config.Config) {
	cm := &c.Features.CertManager
	f.BoolVar(&cm.Active, "cert-manager", cm.Active, "Sets and enables Cert Manager")
	f.StringVar(&cm.Issuer, "cert-manager-issuer", cm.Issuer, "Cert Manager issuer (cluster-selfsigned, …)")
	f.StringVar(&cm.Helm.Image, "cert-manager-image", cm.Helm.Image, "Image for Cert Manager")
	f.StringVar(&cm.Helm.WebhookImage, "cert-manager-webhook-image", cm.Helm.WebhookImage, "Webhook image for Cert Manager")
	f.StringVar(&cm.Helm.CAInjectorImage, "cert-manager-cainjector-image", cm.Helm.CAInjectorImage, "CA injector image for Cert Manager")
	f.StringVar(&cm.Helm.AcmeSolverImage, "cert-manager-acme-solver-image", cm.Helm.AcmeSolverImage, "ACME solver image for Cert Manager")
	f.StringVar(&cm.Helm.StartupAPICheckImage, "cert-manager-startup-api-check-image", cm.Helm.StartupAPICheckImage, "Startup API check image for Cert Manager")
}

func registerContentFlags(f *pflag.FlagSet, c *config.Config) {
	co := &c.Content
	f.BoolVar(&co.UseWhitelist, "content-whitelist", co.UseWhitelist, "Enables the whitelist for statics in content templating")
}
