// Package monitoring is the Go port of com.cloudogu.gitops.tools.Monitoring.
//
// The Groovy original renders a Freemarker .ftl helm-values file. Here we
// compose the same values programmatically, which avoids carrying the
// .ftl→.tmpl conversion through every release of the chart and makes
// every value its own testable expression.
//
// This file ports prometheus-stack-helm-values.ftl.yaml. The Groovy class
// also creates secrets imperatively, applies the ServiceMonitor CRD,
// generates RBAC and NetworkPolicy templates and commits dashboards to the
// cluster-resources git repo — those side effects belong to the runner and
// will land in subsequent phases. The .ftl is the only piece this feature
// owns end-to-end.
package monitoring

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
	"github.com/cloudogu/gitops-playground/go/internal/feature"
)

const (
	releaseName = "kube-prometheus-stack"
	repoName    = "prometheus-community"
	defaultNS   = "monitoring"
)

// MetricsEndpoint describes a Prometheus scrape target. SCM and Jenkins
// values are injected by the runner because both depend on whether the
// component is internal (cluster service URL) or external (configured URL).
// An empty Host means "do not emit a scrape config for this target".
type MetricsEndpoint struct {
	Protocol string
	Host     string
	Path     string
	// Username is only used for the Jenkins target. SCM uses
	// "<namePrefix>metrics" derived from Application.NamePrefix.
	Username string
}

// Feature implements feature.Feature for kube-prometheus-stack.
type Feature struct {
	Deploy deployment.Strategy
	Images feature.ImagePullSecretCreator
	// OpenShiftUID is the numeric UID discovered from the namespace
	// annotation when running on OpenShift. Empty otherwise. The runner
	// fills this in before Install — Monitoring.findValidOpenShiftUid in
	// the Groovy port.
	OpenShiftUID string
	// SCM and Jenkins are the prometheus scrape targets the runner builds
	// from the active SCM/Jenkins configuration. Both are optional.
	SCM     MetricsEndpoint
	Jenkins MetricsEndpoint
	// SCMProviderType comes from config.scm.scmProviderType. The .ftl
	// only emits a scm-manager job when this equals "scm_manager".
	SCMProviderType string
}

// Name implements feature.Feature.
func (Feature) Name() string { return "monitoring" }

// Order is 80 — between SCMM/Jenkins and ArgoCD. The Groovy class is
// @Order(300) on a different ordering axis (post-ArgoCD bootstrap); the
// Go runner picks numbers by deployment phase, so the prometheus stack
// goes up before ArgoCD picks up the cluster-resources repo.
func (Feature) Order() int { return 80 }

// IsEnabled implements feature.Feature.
func (Feature) IsEnabled(cfg *config.Config) bool { return cfg.Features.Monitoring.Active }

// Namespace returns "<namePrefix>monitoring".
func (Feature) Namespace(cfg *config.Config) string {
	return cfg.Application.NamePrefix + defaultNS
}

// Disable is a no-op for the imperatively-installed kube-prometheus-stack.
func (Feature) Disable(_ context.Context, _ *config.Config) error { return nil }

// Install installs (or upgrades) the kube-prometheus-stack chart.
func (f Feature) Install(ctx context.Context, cfg *config.Config) error {
	ns := f.Namespace(cfg)

	if f.Images != nil {
		if err := feature.EnsureProxyRegistryPullSecret(ctx, f.Images, cfg, ns); err != nil {
			return err
		}
	}

	values := f.buildValues(cfg)
	valuesPath, cleanup, err := deployment.RenderHelmValues(cfg, deployment.HelmValuesRequest{
		InlineValues: cfg.Features.Monitoring.Helm.Values,
		ExtraValues:  values,
	})
	if err != nil {
		return fmt.Errorf("monitoring: render values: %w", err)
	}
	defer cleanup()

	return f.Deploy.Deploy(ctx, deployment.Spec{
		RepoURL:        cfg.Features.Monitoring.Helm.RepoURL,
		RepoName:       repoName,
		ChartOrPath:    cfg.Features.Monitoring.Helm.Chart,
		Version:        cfg.Features.Monitoring.Helm.Version,
		Namespace:      ns,
		ReleaseName:    releaseName,
		HelmValuesPath: valuesPath,
		RepoType:       deployment.RepoHelm,
	})
}

// buildValues composes the helm values for kube-prometheus-stack. The map
// is split into three independently testable helpers — grafanaValues,
// prometheusValues, alertManagerValues — plus a small head that mirrors
// the top-level FTL conditionals (crds / global / defaultRules / disabled
// subcharts).
func (f Feature) buildValues(cfg *config.Config) map[string]any {
	app := cfg.Application
	v := map[string]any{}

	// <#if config.application.skipCrds == true>
	if app.SkipCRDs {
		v["crds"] = map[string]any{"enabled": false}
	}

	// <#if config.application.namespaceIsolation || config.registry.createImagePullSecrets>
	if app.NamespaceIsolation || cfg.Registry.CreateImagePullSecrets {
		global := map[string]any{}
		if cfg.Registry.CreateImagePullSecrets {
			global["imagePullSecrets"] = []any{map[string]any{"name": "proxy-registry"}}
		}
		if app.NamespaceIsolation {
			global["rbac"] = map[string]any{"create": false}
		}
		v["global"] = global
		if app.NamespaceIsolation {
			// Avoid 403 in prometheus targets due to missing RBAC in
			// isolated mode.
			v["kubeApiServer"] = map[string]any{"enabled": false}
		}
	}

	// The big static "disable nearly everything" block — see the comment
	// in the .ftl: each image has to be replicated individually in
	// airgapped envs, so we start small.
	v["defaultRules"] = map[string]any{"rules": defaultRulesEnabled()}
	v["kubeStateMetrics"] = map[string]any{"enabled": false}
	v["nodeExporter"] = map[string]any{"enabled": false}

	v["prometheusOperator"] = prometheusOperatorValues(cfg)

	// All of these subcharts are disabled in the .ftl.
	for _, k := range []string{
		"kubelet", "kubeControllerManager", "coreDns", "kubeDns",
		"kubeEtcd", "kubeScheduler", "kubeProxy",
	} {
		v[k] = map[string]any{"enabled": false}
	}
	v["alertmanager"] = alertManagerValues()
	v["grafana"] = f.grafanaValues(cfg)
	v["prometheus"] = f.prometheusValues(cfg)

	return v
}

// alertManagerValues mirrors the `alertmanager:` block. The .ftl only
// disables it — we expose the helper for symmetry with grafanaValues and
// prometheusValues, and to give callers a place to extend the block
// without touching buildValues.
func alertManagerValues() map[string]any {
	return map[string]any{"enabled": false}
}

// prometheusOperatorValues mirrors the `prometheusOperator:` block.
func prometheusOperatorValues(cfg *config.Config) map[string]any {
	app := cfg.Application
	helm := cfg.Features.Monitoring.Helm

	op := map[string]any{
		"enabled": true,
		"admissionWebhooks": map[string]any{
			// Avoids "remote error: tls: bad certificate" in the operator log.
			"enabled": false,
		},
		"tls": map[string]any{
			// Avoids "server TLS client verification disabled" warning.
			"enabled": false,
		},
	}

	if app.Openshift {
		op["securityContext"] = map[string]any{
			"fsGroup":    nil,
			"runAsGroup": nil,
			"runAsUser":  nil,
		}
	}

	if app.NamespaceIsolation {
		op["kubeletService"] = map[string]any{"enabled": false}
		additional := []any{}
		for _, ns := range cfg.Application.Namespaces.Active() {
			additional = append(additional, ns)
		}
		op["namespaces"] = map[string]any{
			"releaseNamespace": false,
			"additional":       additional,
		}
	}

	if app.PodResources {
		op["resources"] = map[string]any{
			"limits":   map[string]any{"cpu": "300m", "memory": "80Mi"},
			"requests": map[string]any{"cpu": "20m", "memory": "40Mi"},
		}
	}

	if img := helm.PrometheusOperatorImage; img != "" {
		op["image"] = dockerImage(img)
	}

	if helm.PrometheusConfigReloaderImage != "" || app.PodResources {
		reloader := map[string]any{}
		if helm.PrometheusConfigReloaderImage != "" {
			reloader["image"] = dockerImage(helm.PrometheusConfigReloaderImage)
		}
		if app.PodResources {
			reloader["resources"] = map[string]any{
				"requests": map[string]any{"cpu": "200m", "memory": "50Mi"},
				"limits":   map[string]any{"cpu": "200m", "memory": "50Mi"},
			}
		}
		op["prometheusConfigReloader"] = reloader
	}

	return op
}

// grafanaValues mirrors the `grafana:` block. The mail/SMTP sub-block is
// the Groovy "alertmanager email block" referenced in the porting notes —
// alertmanager is disabled, so all email notifications run through grafana.
func (f Feature) grafanaValues(cfg *config.Config) map[string]any {
	app := cfg.Application
	mon := cfg.Features.Monitoring
	helm := mon.Helm

	g := map[string]any{
		"grafana.ini": map[string]any{
			"analytics": map[string]any{"check_for_updates": false},
		},
		"defaultDashboardsEnabled": false,
		"adminUser":                app.Username,
		"adminPassword":            app.Password,
		"service":                  map[string]any{"type": "ClusterIP"},
	}

	if app.Openshift && f.OpenShiftUID != "" {
		g["securityContext"] = map[string]any{
			"fsGroup":    f.OpenShiftUID,
			"runAsGroup": f.OpenShiftUID,
			"runAsUser":  f.OpenShiftUID,
		}
	}
	if app.NamespaceIsolation {
		// We add roles and role bindings to each namespace manually.
		g["rbac"] = map[string]any{"create": false}
	}

	if host := grafanaHost(mon.GrafanaURL); host != "" {
		ing := map[string]any{
			"enabled": true,
			"hosts":   []any{host},
		}
		if cfg.Features.CertManager.Active {
			ing["annotations"] = map[string]any{
				"cert-manager.io/cluster-issuer": cfg.Features.CertManager.Issuer,
			}
			ing["tls"] = []any{
				map[string]any{
					"secretName": "grafana-tls",
					"hosts":      []any{host},
				},
			}
		}
		g["ingress"] = ing
	}

	if helm.GrafanaImage != "" {
		g["image"] = dockerImage(helm.GrafanaImage)
	}

	// sidecar.dashboards block. searchNamespace becomes the list of active
	// namespaces when isolation is on (originally an FTL `<#list>` joined
	// by comma), otherwise the literal "ALL".
	sidecar := map[string]any{
		"dashboards": map[string]any{
			"labelValue":      "1",
			"searchNamespace": searchNamespace(cfg),
		},
	}
	if app.PodResources {
		sidecar["resources"] = map[string]any{
			"limits":   map[string]any{"cpu": "100m", "memory": "200Mi"},
			"requests": map[string]any{"cpu": "35m", "memory": "65Mi"},
		}
	}
	if helm.GrafanaSidecarImage != "" {
		sidecar["image"] = dockerImage(helm.GrafanaSidecarImage)
	}
	g["sidecar"] = sidecar

	// Mail / SMTP integration. The Groovy template wires email notifiers,
	// alerting contact points, optional smtp.existingSecret and the
	// GF_SMTP_* env block. We mirror the structure 1:1.
	if cfg.Features.Mail.Active {
		mergeGrafanaMail(g, cfg)
	}

	if app.PodResources {
		g["resources"] = map[string]any{
			"limits":   map[string]any{"cpu": "1", "memory": "140Mi"},
			"requests": map[string]any{"cpu": "350m", "memory": "70Mi"},
		}
	}

	return g
}

// mergeGrafanaMail emits the notifiers/alerting/smtp/env keys driven by
// config.features.mail.*. Kept separate because the FTL block is long
// and the structure is easier to read in isolation.
func mergeGrafanaMail(g map[string]any, cfg *config.Config) {
	mon := cfg.Features.Monitoring
	mail := cfg.Features.Mail

	g["notifiers"] = map[string]any{
		"notifiers.yaml": map[string]any{
			"notifiers": []any{
				map[string]any{
					"name":       "mail",
					"type":       "email",
					"uid":        "email1",
					"is_default": true,
					"settings": map[string]any{
						"addresses":   mon.GrafanaEmailTo,
						"uploadImage": false,
					},
				},
			},
		},
	}

	g["alerting"] = map[string]any{
		"contactpoints.yaml": map[string]any{
			"apiVersion": 1,
			"contactPoints": []any{
				map[string]any{
					"orgId":      1,
					"name":       "email",
					"is_default": true,
					"receivers": []any{
						map[string]any{
							"uid":      "email1",
							"type":     "email",
							"settings": map[string]any{"addresses": mon.GrafanaEmailTo},
						},
					},
				},
			},
		},
		"notification-policies.yaml": map[string]any{
			"apiVersion": 1,
			"policies": []any{
				map[string]any{
					"orgId":      1,
					"is_default": true,
					"receiver":   "email",
					"routes": []any{
						map[string]any{"receiver": "email"},
					},
					"group_by": []any{"grafana_folder", "alertname"},
				},
			},
		},
	}

	if mail.SMTPUser != "" || mail.SMTPPassword != "" {
		g["smtp"] = map[string]any{
			// References an externally-created secret; see the
			// Groovy setupMonitoringSecrets / runner phase.
			"existingSecret": "grafana-email-secret",
		}
	}

	host := mail.SMTPAddress
	if mail.SMTPPort != 0 {
		host = fmt.Sprintf("%s:%d", host, mail.SMTPPort)
	}
	g["env"] = map[string]any{
		"GF_SMTP_ENABLED":      true,
		"GF_SMTP_FROM_ADDRESS": mon.GrafanaEmailFrom,
		"GF_SMTP_HOST":         host,
	}
}

// prometheusValues mirrors the `prometheus:` block.
func (f Feature) prometheusValues(cfg *config.Config) map[string]any {
	app := cfg.Application
	helm := cfg.Features.Monitoring.Helm

	spec := map[string]any{
		"serviceMonitorNamespaceSelector":         namespaceMatchExpression(cfg),
		"serviceMonitorSelectorNilUsesHelmValues": false,
		"podMonitorNamespaceSelector":             namespaceMatchExpression(cfg),
		"podMonitorSelectorNilUsesHelmValues":     false,
		"ruleNamespaceSelector":                   namespaceMatchExpression(cfg),
		"ruleSelectorNilUsesHelmValues":           false,
		"scrapeConfigSelectorNilUsesHelmValues":   false,
		"probeNamespaceSelector":                  namespaceMatchExpression(cfg),
		"probeSelectorNilUsesHelmValues":          false,
		"secrets": []any{
			"prometheus-metrics-creds-scmm",
			"prometheus-metrics-creds-jenkins",
		},
	}

	if app.Openshift {
		spec["automountServiceAccountToken"] = nil
		spec["securityContext"] = map[string]any{
			"fsGroup":    nil,
			"runAsGroup": nil,
			"runAsUser":  nil,
		}
	}
	if app.PodResources {
		spec["resources"] = map[string]any{
			"limits":   map[string]any{"cpu": "500m", "memory": "1Gi"},
			"requests": map[string]any{"cpu": "50m", "memory": "450Mi"},
		}
	}
	if helm.PrometheusImage != "" {
		spec["image"] = dockerImage(helm.PrometheusImage)
	}
	if jobs := f.additionalScrapeConfigs(cfg); len(jobs) > 0 {
		spec["additionalScrapeConfigs"] = jobs
	}

	return map[string]any{"prometheusSpec": spec}
}

// additionalScrapeConfigs builds the static scrape targets for SCM
// Manager and Jenkins. The Groovy code injects the URLs via Freemarker
// `addHelmValuesData` (see scmConfigurationMetrics / jenkinsConfigurationMetrics);
// here the runner fills in f.SCM and f.Jenkins instead.
func (f Feature) additionalScrapeConfigs(cfg *config.Config) []any {
	var out []any

	if strings.EqualFold(f.SCMProviderType, "scm_manager") &&
		f.SCM.Host != "" && f.SCM.Protocol != "" && f.SCM.Path != "" {
		out = append(out, map[string]any{
			"job_name":       "scm-manager",
			"static_configs": []any{map[string]any{"targets": []any{f.SCM.Host}}},
			"scheme":         f.SCM.Protocol,
			"metrics_path":   f.SCM.Path,
			"basic_auth": map[string]any{
				"username":      cfg.Application.NamePrefix + "metrics",
				"password_file": "/etc/prometheus/secrets/prometheus-metrics-creds-scmm/password",
			},
		})
	}

	if cfg.Jenkins.Active && f.Jenkins.Host != "" {
		out = append(out, map[string]any{
			"job_name":       "jenkins",
			"static_configs": []any{map[string]any{"targets": []any{f.Jenkins.Host}}},
			"scheme":         f.Jenkins.Protocol,
			"metrics_path":   f.Jenkins.Path,
			"basic_auth": map[string]any{
				"username":      f.Jenkins.Username,
				"password_file": "/etc/prometheus/secrets/prometheus-metrics-creds-jenkins/password",
			},
		})
	}

	return out
}

// defaultRulesEnabled mirrors the long defaultRules.rules block. Only a
// handful of rules stay on; everything else is explicitly false so the
// helm chart's defaults don't sneak back in on upgrade.
func defaultRulesEnabled() map[string]any {
	return map[string]any{
		"general":                           true,
		"prometheus":                        true,
		"prometheusOperator":                true,
		"kubePrometheusGeneral":             true,
		"alertmanager":                      false,
		"etcd":                              false,
		"k8sContainerCpuUsageSecondsTotal":  false,
		"k8sContainerMemoryCache":           false,
		"k8sContainerMemoryRss":             false,
		"k8sContainerMemorySwap":            false,
		"k8sContainerResource":              false,
		"k8sContainerMemoryWorkingSetBytes": false,
		"k8sPodOwner":                       false,
		"kubeApiserver":                     false,
		"kubeApiserverAvailability":         false,
		"kubeApiserverBurnrate":             false,
		"kubeApiserverHistogram":            false,
		"kubeApiserverSlos":                 false,
		"kubelet":                           false,
		"kubePrometheusNodeRecording":       false,
		"kubernetesAbsent":                  false,
		"kubernetesApps":                    false,
		"kubernetesResources":               false,
		"kubernetesStorage":                 false,
		"kubernetesSystem":                  false,
		"kubeSchedulerAlerting":             false,
		"kubeSchedulerRecording":            false,
		"kubeStateMetrics":                  false,
		"network":                           false,
		"node":                              false,
		"nodeExporterAlerting":              false,
		"nodeExporterRecording":             false,
		"windows":                           false,
	}
}

// namespaceMatchExpression returns the matchExpressions block used by the
// four Prometheus *NamespaceSelector keys. The FTL renders `{}` when no
// namespaces are active — an empty `values:` list achieves the same
// "match nothing" semantics in a typed Go map without resorting to a
// magic literal.
func namespaceMatchExpression(cfg *config.Config) map[string]any {
	values := []any{}
	for _, ns := range cfg.Application.Namespaces.Active() {
		values = append(values, ns)
	}
	return map[string]any{
		"matchExpressions": []any{
			map[string]any{
				"key":      "kubernetes.io/metadata.name",
				"operator": "In",
				"values":   values,
			},
		},
	}
}

// searchNamespace mirrors `<#list namespaces as namespace>${namespace}<#if namespace_has_next>,</#if></#list>`.
// When namespace isolation is off, grafana scans every namespace ("ALL").
func searchNamespace(cfg *config.Config) string {
	if !cfg.Application.NamespaceIsolation {
		return "ALL"
	}
	return strings.Join(cfg.Application.Namespaces.Active(), ",")
}

// grafanaHost extracts the URL host from config.features.monitoring.grafanaUrl.
// Mirrors the Groovy `new URL(...).host` call; invalid or empty URLs return "".
func grafanaHost(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u == nil {
		return ""
	}
	return u.Host
}

// dockerImage mirrors DockerImageParser.parse(...) from the Groovy code:
// split off the tag at the last ":" then split the remainder on "/" so
// that the last two segments are the repository and everything before is
// the registry. The Groovy parser throws when no tag is present; we keep
// the empty tag for callers to detect and fail explicitly in the runner.
func dockerImage(ref string) map[string]any {
	registry, repo, tag := parseImage(ref)
	return map[string]any{
		"registry":   registry,
		"repository": repo,
		"tag":        tag,
	}
}

// parseImage returns (registry, repository, tag). See dockerImage for
// the contract. "docker.io/library/foo:tag" → ("docker.io", "library/foo", "tag");
// "foo/bar:tag" → ("", "foo/bar", "tag"); "foo:tag" → ("", "foo", "tag").
func parseImage(ref string) (string, string, string) {
	// 1. Split off the tag at the last ":".
	idx := strings.LastIndex(ref, ":")
	var imageWithoutTag, tag string
	if idx < 0 {
		imageWithoutTag = ref
	} else {
		imageWithoutTag = ref[:idx]
		tag = ref[idx+1:]
	}

	// 2. Split the path; last two segments = repository, rest = registry.
	parts := strings.Split(imageWithoutTag, "/")
	switch {
	case len(parts) == 1:
		return "", parts[0], tag
	case len(parts) == 2:
		return "", strings.Join(parts, "/"), tag
	default:
		registry := strings.Join(parts[:len(parts)-2], "/")
		repo := strings.Join(parts[len(parts)-2:], "/")
		return registry, repo, tag
	}
}
