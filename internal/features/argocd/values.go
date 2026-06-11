package argocd

// values.go composes the helm values for the umbrella `argo-cd` chart in
// the non-operator code path. It is the programmatic counterpart of
// argocd/cluster-resources/apps/argocd/argocd/values.ftl.yaml.
//
// The .ftl renders a *top-level* `argo-cd:` key that nests every sub-chart
// value beneath it (because the umbrella chart references `argo-cd` as a
// dependency). We keep the same shape here: buildValues returns
// {"argo-cd": <subValues>}.
//
// The values are split into one helper per logical block so each is
// independently testable:
//
//   - argoCDServerValues       – chart key `server`
//   - controllerValues         – chart key `controller`
//   - repoServerValues         – chart key `repoServer`
//   - notificationsValues      – chart key `notifications`
//   - operatorValues           – the ArgoCD CR spec for operator mode
//     (not part of the helm values; produced separately for the runner
//     to apply via kubectl apply -f).
//
// Field order intentionally tracks the .ftl line-by-line so a diff
// between the two files in code review stays narrow.

import (
	"fmt"
	"net/url"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

// buildValues composes the umbrella chart values. The result is meant to
// be passed verbatim into deployment.HelmValuesRequest.ExtraValues.
func buildValues(cfg *config.Config) map[string]any {
	sub := map[string]any{}

	// <#if config.application.skipCrds==true>
	if cfg.Application.SkipCRDs {
		sub["crds"] = map[string]any{"install": false}
	}
	// <#if config.application.netpols == true>
	if cfg.Application.NetPols {
		sub["global"] = map[string]any{
			"networkPolicy": map[string]any{"create": true},
		}
	}

	sub["server"] = argoCDServerValues(cfg)
	sub["controller"] = controllerValues(cfg)
	sub["repoServer"] = repoServerValues(cfg)
	sub["configs"] = configsValues(cfg)
	sub["notifications"] = notificationsValues(cfg)

	return map[string]any{"argo-cd": sub}
}

// argoCDServerValues mirrors the `server:` block. Holds ingress,
// service, metrics and the metrics ServiceMonitor when monitoring is on.
func argoCDServerValues(cfg *config.Config) map[string]any {
	s := map[string]any{
		"service": map[string]any{"type": "ClusterIP"},
		"metrics": metricsBlock(cfg),
	}
	if host := argocdHost(cfg.Features.ArgoCD.URL); host != "" {
		ing := map[string]any{
			"enabled":  true,
			"hostname": host,
		}
		if cfg.Features.CertManager.Active {
			ing["annotations"] = map[string]any{
				"cert-manager.io/cluster-issuer": cfg.Features.CertManager.Issuer,
			}
			ing["tls"] = true
		}
		s["ingress"] = ing
	}
	return s
}

// controllerValues mirrors the `controller:` block (metrics only).
func controllerValues(cfg *config.Config) map[string]any {
	return map[string]any{"metrics": metricsBlock(cfg)}
}

// repoServerValues mirrors the `repoServer:` block (metrics only).
func repoServerValues(cfg *config.Config) map[string]any {
	return map[string]any{"metrics": metricsBlock(cfg)}
}

// metricsBlock builds the `metrics:` sub-block used identically by the
// three components above. ServiceMonitor is only emitted when monitoring
// is active.
func metricsBlock(cfg *config.Config) map[string]any {
	m := map[string]any{"enabled": true}
	if cfg.Features.Monitoring.Active {
		m["serviceMonitor"] = map[string]any{
			"enabled":   true,
			"namespace": cfg.Application.NamePrefix + "monitoring",
			"selector":  map[string]any{"release": "kube-prometheus-stack"},
		}
	}
	return m
}

// configsValues mirrors the `configs:` block: params + cm. The .ftl
// hard-codes server.insecure=true because TLS is terminated in the
// ingress; we keep the same default to preserve behaviour.
func configsValues(cfg *config.Config) map[string]any {
	return map[string]any{
		"params": map[string]any{
			// Needed to enable deploying the Application resource into other
			// namespaces than argocd (see Groovy comment).
			"application.namespaces": cfg.Application.NamePrefix + defaultNamespace,
			"server.insecure":        true,
		},
		"cm": map[string]any{
			"timeout.reconciliation":    "15s",
			"repository.check.interval": "30s",
		},
	}
}

// notificationsValues mirrors the `notifications:` block. The
// templates/triggers sub-tree is identical to the upstream chart's
// "default" notifications config – we model it as a literal map so it is
// directly testable. The freemarker `<#noparse>` block in the .ftl is
// just an escape for `{{ .app... }}` interpolations meant for ArgoCD
// itself, not for the template engine.
func notificationsValues(cfg *config.Config) map[string]any {
	mail := cfg.Features.Mail

	n := map[string]any{
		"secret":    map[string]any{"create": false},
		"enabled":   mail.Active,
		"argocdUrl": cfg.Features.ArgoCD.URL,
	}

	if !mail.Active {
		return n
	}

	// Notifiers: either user-supplied SMTP host, or the in-cluster
	// "mail" service in the monitoring namespace (matches the .ftl).
	smtp := "host: mail." + cfg.Application.NamePrefix + "monitoring.svc.cluster.local\nport: 1025\n"
	if mail.SMTPAddress != "" {
		smtp = "host: " + mail.SMTPAddress + "\n"
		if mail.SMTPPort != 0 {
			smtp += fmt.Sprintf("port: %d\n", mail.SMTPPort)
		}
		if mail.SMTPUser != "" {
			smtp += "username: $email-username\n"
		}
		if mail.SMTPPassword != "" {
			smtp += "password: $email-password\n"
		}
	}
	smtp += "from: " + cfg.Features.ArgoCD.EmailFrom + "\n"

	n["notifiers"] = map[string]any{"service.email": smtp}
	n["templates"] = notificationTemplates()
	n["triggers"] = notificationTriggers()

	return n
}

// operatorValues composes the spec map that the runner serialises into
// the operator/argocd.yaml override. The Groovy code patches this file
// in-place via MapUtils.deepMerge with config.features.argocd.values;
// we let the runner do the same against the rendered file.
//
// It is intentionally NOT plugged into buildValues – operator mode
// never installs the umbrella chart. Kept here so the operator spec and
// the helm spec sit side-by-side and stay easy to compare.
//
//nolint:unused // Consumed by the operator install path (REMAINING P1).
func operatorValues(cfg *config.Config) map[string]any {
	envBlock := func() []any {
		if len(cfg.Features.ArgoCD.Env) == 0 {
			return nil
		}
		out := make([]any, 0, len(cfg.Features.ArgoCD.Env))
		for _, e := range cfg.Features.ArgoCD.Env {
			out = append(out, map[string]any{"name": e["name"], "value": e["value"]})
		}
		return out
	}
	envs := envBlock()

	withEnv := func(m map[string]any) map[string]any {
		if envs != nil {
			m["env"] = envs
		}
		return m
	}

	spec := map[string]any{
		"applicationSet": withEnv(map[string]any{
			"enabled": true,
			"resources": map[string]any{
				"limits":   map[string]any{"cpu": "2", "memory": "1Gi"},
				"requests": map[string]any{"cpu": "250m", "memory": "512Mi"},
			},
		}),
		"notifications": withEnv(map[string]any{
			"enabled": true,
			"resources": map[string]any{
				"limits":   map[string]any{"cpu": "100m", "memory": "128Mi"},
				"requests": map[string]any{"cpu": "100m", "memory": "128Mi"},
			},
		}),
		"controller": withEnv(map[string]any{
			"resources": map[string]any{
				"limits":   map[string]any{"cpu": "2000m", "memory": "2048Mi"},
				"requests": map[string]any{"cpu": "250m", "memory": "1024Mi"},
			},
		}),
		"ha": map[string]any{
			"enabled": false,
			"resources": map[string]any{
				"limits":   map[string]any{"cpu": "500m", "memory": "256Mi"},
				"requests": map[string]any{"cpu": "250m", "memory": "128Mi"},
			},
		},
		"redis": map[string]any{
			"resources": map[string]any{
				"limits":   map[string]any{"cpu": "500m", "memory": "256Mi"},
				"requests": map[string]any{"cpu": "250m", "memory": "128Mi"},
			},
		},
		"repo": withEnv(map[string]any{
			"resources": map[string]any{
				"limits":   map[string]any{"cpu": "1000m", "memory": "1024Mi"},
				"requests": map[string]any{"cpu": "250m", "memory": "256Mi"},
			},
		}),
		"server": withEnv(map[string]any{
			"insecure": cfg.Application.Insecure,
			"resources": map[string]any{
				"limits":   map[string]any{"cpu": "500m", "memory": "256Mi"},
				"requests": map[string]any{"cpu": "125m", "memory": "128Mi"},
			},
			"route":   map[string]any{"enabled": cfg.Application.Openshift},
			"host":    argocdHost(cfg.Features.ArgoCD.URL),
			"ingress": map[string]any{"enabled": !cfg.Application.Openshift && !cfg.Application.Insecure},
		}),
	}

	if cfg.Application.Openshift {
		spec["sso"] = map[string]any{
			"dex": map[string]any{
				"openShiftOAuth": true,
				"resources": map[string]any{
					"limits":   map[string]any{"cpu": "500m", "memory": "256Mi"},
					"requests": map[string]any{"cpu": "250m", "memory": "128Mi"},
				},
			},
			"provider": "dex",
		}
		spec["rbac"] = map[string]any{
			"defaultPolicy": "",
			"policy": "g, system:cluster-admins, role:admin\n" +
				"g, platform-admin, role:admin\n",
			"scopes": "[groups]",
		}
	}

	return map[string]any{
		"apiVersion": "argoproj.io/v1beta1",
		"kind":       "ArgoCD",
		"metadata": map[string]any{
			"name":      "argocd",
			"namespace": cfg.Application.NamePrefix + defaultNamespace,
		},
		"spec": spec,
	}
}

// argocdHost extracts host from features.argocd.url. Empty / invalid
// URLs return "" — same semantics as the .ftl `argocd.host?has_content`
// guard.
func argocdHost(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u == nil {
		return ""
	}
	return u.Host
}

// notificationTemplates returns the literal templates map embedded in
// the .ftl. The values are upstream ArgoCD template expressions ({{ .app... }})
// that must not be evaluated by Go templating; they are stored as raw
// strings and passed through to helm as-is.
func notificationTemplates() map[string]any {
	return map[string]any{
		"template.app-deployed":               "email:\n  subject: New version of an application {{.app.metadata.name}} is up and running.\nmessage: |\n  Application {{.app.metadata.name}} is now running new version of deployments manifests.\n",
		"template.app-health-degraded":        "email:\n  subject: Application {{.app.metadata.name}} has degraded.\nmessage: |\n  Application {{.app.metadata.name}} has degraded.\n  Application details: {{.context.argocdUrl}}/applications/{{.app.metadata.name}}.\n",
		"template.app-sync-failed":            "email:\n  subject: Failed to sync application {{.app.metadata.name}}.\nmessage: |\n  The sync operation of application {{.app.metadata.name}} has failed at {{.app.status.operationState.finishedAt}} with the following error: {{.app.status.operationState.message}}\n  Sync operation details are available at: {{.context.argocdUrl}}/applications/{{.app.metadata.name}}?operation=true .\n",
		"template.app-sync-running":           "email:\n  subject: Start syncing application {{.app.metadata.name}}.\nmessage: |\n  The sync operation of application {{.app.metadata.name}} has started at {{.app.status.operationState.startedAt}}.\n  Sync operation details are available at: {{.context.argocdUrl}}/applications/{{.app.metadata.name}}?operation=true .\n",
		"template.app-sync-status-unknown":    "email:\n  subject: Application {{.app.metadata.name}} sync status is 'Unknown'\nmessage: |\n  Application {{.app.metadata.name}} sync is 'Unknown'.\n  Application details: {{.context.argocdUrl}}/applications/{{.app.metadata.name}}.\n  {{range $c := .app.status.conditions}}\n      * {{$c.message}}\n  {{end}}\n",
		"template.app-sync-succeeded":         "email:\n  subject: Application {{.app.metadata.name}} has been successfully synced.\nmessage: |\n  Application {{.app.metadata.name}} has been successfully synced at {{.app.status.operationState.finishedAt}}.\n  Sync operation details are available at: {{.context.argocdUrl}}/applications/{{.app.metadata.name}}?operation=true .\n",
		"template.app-sync-status-longer-10s": "email:\n  subject: Application {{.app.metadata.name}} is too long in sync status.\nmessage: |\n  The Application {{.app.metadata.name}} is now longer than 10 seconds in sync status. This may be because one of its resources resides in a SyncFailed status.\n  Sync operation details are available at: {{.context.argocdUrl}}/applications/{{.app.metadata.name}}?operation=true .\n",
	}
}

// notificationTriggers returns the literal triggers map. Same caveat as
// notificationTemplates – the values contain `{{ ... }}` that must be
// passed through to helm unchanged.
func notificationTriggers() map[string]any {
	return map[string]any{
		"defaultTriggers":                   "- on-deleted\n- on-health-degraded\n- on-sync-failed\n",
		"trigger.on-deployed":               "- description: Application is synced and healthy. Triggered once per commit.\n  oncePer: app.status.sync.revision\n  send:\n  - app-deployed\n  when: app.status.operationState.phase in ['Succeeded'] and app.status.health.status == 'Healthy'\n",
		"trigger.on-health-degraded":        "- description: Application has degraded\n  send:\n  - app-health-degraded\n  when: app.status.health.status == 'Degraded'\n",
		"trigger.on-sync-failed":            "- description: Application syncing has failed\n  send:\n  - app-sync-failed\n  when: app.status.operationState.phase in ['Error', 'Failed']\n",
		"trigger.on-sync-running":           "- description: Application is being synced\n  send:\n  - app-sync-running\n  when: app.status.operationState.phase in ['Running']\n",
		"trigger.on-sync-status-unknown":    "- description: Application status is 'Unknown'\n  send:\n  - app-sync-status-unknown\n  when: app.status.sync.status == 'Unknown'\n",
		"trigger.on-sync-succeeded":         "- description: Application syncing has succeeded\n  send:\n  - app-sync-succeeded\n  when: app.status.operationState.phase in ['Succeeded']\n",
		"trigger.on-sync-status-longer-10s": "- description: Application syncing is longer than 10 seconds\n  send:\n  - app-sync-status-longer-10s\n  when: app.status.operationState.phase in ['Running'] and time.Now().Sub(time.Parse(app.status.operationState.startedAt)).Seconds() >= 10\n",
	}
}
