package cli

import (
	"context"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

// loadAndInitConfig executes the same pipeline as the Groovy
// GitopsPlaygroundCli.run does, but in Go:
//
//  1. Take the user's CLI values (already bound into `cli` by Cobra).
//  2. Build the file/config-map/profile baseline via config.Load.
//  3. Apply CLI overrides on top of the baseline (CLI wins).
//  4. Run the configurator (derived flags, baseUrl fan-out, etc.).
//
// The K8s ConfigMap reader is wired in later phases; passing nil disables
// --config-map (and surfaces a friendly error if someone uses it without
// a cluster connection).
func loadAndInitConfig(ctx context.Context, cli *config.Config) (*config.Config, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	merged, err := config.Load(ctx, config.LoadOptions{
		ProfileName: cli.Application.Profile,
		ConfigFiles: cli.Application.ConfigFiles,
		ConfigMaps:  cli.Application.ConfigMaps,
		// K8s: nil for now — Phase 3 plugs the adapter in here.
	})
	if err != nil {
		return nil, err
	}

	applyCLIOverrides(merged, cli)

	if err := config.Initialise(merged); err != nil {
		return nil, err
	}
	return merged, nil
}

// applyCLIOverrides copies the (parsed) CLI values onto `merged`, but only
// for fields that the user actually changed. We use a default Config as
// reference: if `cli.X` differs from the default and from `merged.X`, we
// take the CLI value as the explicit user request.
//
// This is a deliberately simple model. It handles the typical case where
// the user runs `gop --profile=minimal --jenkins=false --base-url=…`
// without needing to know how cobra flag.Changed works for every flag.
func applyCLIOverrides(merged, cli *config.Config) {
	def := config.New()

	// application
	overrideString(&merged.Application.NamePrefix, cli.Application.NamePrefix, def.Application.NamePrefix)
	overrideString(&merged.Application.Username, cli.Application.Username, def.Application.Username)
	overrideString(&merged.Application.Password, cli.Application.Password, def.Application.Password)
	overrideString(&merged.Application.BaseURL, cli.Application.BaseURL, def.Application.BaseURL)
	overrideString(&merged.Application.GitName, cli.Application.GitName, def.Application.GitName)
	overrideString(&merged.Application.GitEmail, cli.Application.GitEmail, def.Application.GitEmail)
	overrideString(&merged.Application.GopNamespace, cli.Application.GopNamespace, def.Application.GopNamespace)
	overrideBool(&merged.Application.Yes, cli.Application.Yes, def.Application.Yes)
	overrideBool(&merged.Application.Debug, cli.Application.Debug, def.Application.Debug)
	overrideBool(&merged.Application.Trace, cli.Application.Trace, def.Application.Trace)
	overrideBool(&merged.Application.Destroy, cli.Application.Destroy, def.Application.Destroy)
	overrideBool(&merged.Application.Insecure, cli.Application.Insecure, def.Application.Insecure)
	overrideBool(&merged.Application.Openshift, cli.Application.Openshift, def.Application.Openshift)
	overrideBool(&merged.Application.PodResources, cli.Application.PodResources, def.Application.PodResources)
	overrideBool(&merged.Application.URLSeparatorHyphen, cli.Application.URLSeparatorHyphen, def.Application.URLSeparatorHyphen)
	overrideBool(&merged.Application.MirrorRepos, cli.Application.MirrorRepos, def.Application.MirrorRepos)
	overrideBool(&merged.Application.SkipCRDs, cli.Application.SkipCRDs, def.Application.SkipCRDs)
	overrideBool(&merged.Application.NamespaceIsolation, cli.Application.NamespaceIsolation, def.Application.NamespaceIsolation)
	overrideBool(&merged.Application.NetPols, cli.Application.NetPols, def.Application.NetPols)
	overrideBool(&merged.Application.ClusterAdmin, cli.Application.ClusterAdmin, def.Application.ClusterAdmin)

	// registry
	overrideBool(&merged.Registry.Active, cli.Registry.Active, def.Registry.Active)
	overrideInt(&merged.Registry.InternalPort, cli.Registry.InternalPort, def.Registry.InternalPort)
	overrideString(&merged.Registry.URL, cli.Registry.URL, def.Registry.URL)
	overrideString(&merged.Registry.Path, cli.Registry.Path, def.Registry.Path)
	overrideString(&merged.Registry.Username, cli.Registry.Username, def.Registry.Username)
	overrideString(&merged.Registry.Password, cli.Registry.Password, def.Registry.Password)
	overrideString(&merged.Registry.ProxyURL, cli.Registry.ProxyURL, def.Registry.ProxyURL)
	overrideString(&merged.Registry.ProxyPath, cli.Registry.ProxyPath, def.Registry.ProxyPath)
	overrideString(&merged.Registry.ProxyUsername, cli.Registry.ProxyUsername, def.Registry.ProxyUsername)
	overrideString(&merged.Registry.ProxyPassword, cli.Registry.ProxyPassword, def.Registry.ProxyPassword)
	overrideString(&merged.Registry.ReadOnlyUsername, cli.Registry.ReadOnlyUsername, def.Registry.ReadOnlyUsername)
	overrideString(&merged.Registry.ReadOnlyPassword, cli.Registry.ReadOnlyPassword, def.Registry.ReadOnlyPassword)
	overrideBool(&merged.Registry.CreateImagePullSecrets, cli.Registry.CreateImagePullSecrets, def.Registry.CreateImagePullSecrets)

	// jenkins
	overrideBool(&merged.Jenkins.Active, cli.Jenkins.Active, def.Jenkins.Active)
	overrideBool(&merged.Jenkins.SkipRestart, cli.Jenkins.SkipRestart, def.Jenkins.SkipRestart)
	overrideBool(&merged.Jenkins.SkipPlugins, cli.Jenkins.SkipPlugins, def.Jenkins.SkipPlugins)
	overrideString(&merged.Jenkins.URL, cli.Jenkins.URL, def.Jenkins.URL)
	overrideString(&merged.Jenkins.Username, cli.Jenkins.Username, def.Jenkins.Username)
	overrideString(&merged.Jenkins.Password, cli.Jenkins.Password, def.Jenkins.Password)
	overrideString(&merged.Jenkins.MetricsUsername, cli.Jenkins.MetricsUsername, def.Jenkins.MetricsUsername)
	overrideString(&merged.Jenkins.MetricsPassword, cli.Jenkins.MetricsPassword, def.Jenkins.MetricsPassword)
	overrideString(&merged.Jenkins.MavenCentralMirror, cli.Jenkins.MavenCentralMirror, def.Jenkins.MavenCentralMirror)
	if len(cli.Jenkins.AdditionalEnvs) > 0 {
		merged.Jenkins.AdditionalEnvs = cli.Jenkins.AdditionalEnvs
	}

	// argocd
	overrideBool(&merged.Features.ArgoCD.Active, cli.Features.ArgoCD.Active, def.Features.ArgoCD.Active)
	overrideBool(&merged.Features.ArgoCD.Operator, cli.Features.ArgoCD.Operator, def.Features.ArgoCD.Operator)
	overrideString(&merged.Features.ArgoCD.URL, cli.Features.ArgoCD.URL, def.Features.ArgoCD.URL)
	overrideString(&merged.Features.ArgoCD.EmailFrom, cli.Features.ArgoCD.EmailFrom, def.Features.ArgoCD.EmailFrom)
	overrideString(&merged.Features.ArgoCD.EmailToUser, cli.Features.ArgoCD.EmailToUser, def.Features.ArgoCD.EmailToUser)
	overrideString(&merged.Features.ArgoCD.EmailToAdmin, cli.Features.ArgoCD.EmailToAdmin, def.Features.ArgoCD.EmailToAdmin)
	overrideString(&merged.Features.ArgoCD.ResourceInclusionsCluster, cli.Features.ArgoCD.ResourceInclusionsCluster, def.Features.ArgoCD.ResourceInclusionsCluster)
	overrideString(&merged.Features.ArgoCD.Namespace, cli.Features.ArgoCD.Namespace, def.Features.ArgoCD.Namespace)

	// monitoring
	overrideBool(&merged.Features.Monitoring.Active, cli.Features.Monitoring.Active, def.Features.Monitoring.Active)
	overrideString(&merged.Features.Monitoring.GrafanaURL, cli.Features.Monitoring.GrafanaURL, def.Features.Monitoring.GrafanaURL)
	overrideString(&merged.Features.Monitoring.GrafanaEmailFrom, cli.Features.Monitoring.GrafanaEmailFrom, def.Features.Monitoring.GrafanaEmailFrom)
	overrideString(&merged.Features.Monitoring.GrafanaEmailTo, cli.Features.Monitoring.GrafanaEmailTo, def.Features.Monitoring.GrafanaEmailTo)

	// secrets
	overrideString(&merged.Features.Secrets.Vault.Mode, cli.Features.Secrets.Vault.Mode, def.Features.Secrets.Vault.Mode)
	overrideString(&merged.Features.Secrets.Vault.URL, cli.Features.Secrets.Vault.URL, def.Features.Secrets.Vault.URL)

	// mail
	overrideString(&merged.Features.Mail.SMTPAddress, cli.Features.Mail.SMTPAddress, def.Features.Mail.SMTPAddress)
	overrideInt(&merged.Features.Mail.SMTPPort, cli.Features.Mail.SMTPPort, def.Features.Mail.SMTPPort)
	overrideString(&merged.Features.Mail.SMTPUser, cli.Features.Mail.SMTPUser, def.Features.Mail.SMTPUser)
	overrideString(&merged.Features.Mail.SMTPPassword, cli.Features.Mail.SMTPPassword, def.Features.Mail.SMTPPassword)

	// ingress / certManager / content
	overrideBool(&merged.Features.Ingress.Active, cli.Features.Ingress.Active, def.Features.Ingress.Active)
	overrideBool(&merged.Features.CertManager.Active, cli.Features.CertManager.Active, def.Features.CertManager.Active)
	overrideString(&merged.Features.CertManager.Issuer, cli.Features.CertManager.Issuer, def.Features.CertManager.Issuer)
	overrideBool(&merged.Content.UseWhitelist, cli.Content.UseWhitelist, def.Content.UseWhitelist)
}

func overrideString(target *string, cliVal, defVal string) {
	if cliVal != defVal {
		*target = cliVal
	}
}

func overrideBool(target *bool, cliVal, defVal bool) {
	if cliVal != defVal {
		*target = cliVal
	}
}

func overrideInt(target *int, cliVal, defVal int) {
	if cliVal != defVal {
		*target = cliVal
	}
}
