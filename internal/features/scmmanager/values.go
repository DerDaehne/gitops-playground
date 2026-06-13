// values.go is the programmatic Go counterpart of
// argocd/cluster-resources/apps/scm-manager/templates/values.ftl.yaml.
//
// The Groovy original rendered Freemarker; we compose the same values
// programmatically so each field is its own testable expression. The
// .ftl mapped as follows:
//
//	always                                           → persistence, livenessProbe,
//	                                                   fullnameOverride, service,
//	                                                   extraEnv (INITIAL{USER,PASSWORD})
//	<#if host?has_content>                           → ingress (hosts, path)
//	<#if config.features.certManager.active == true> → ingress.annotations + tls
//
// The two extraEnv values come from scm.scmManager.{username,password} –
// the same fields the Groovy code passes as `username`/`password` template
// variables (see ScmManagerSetup.setupHelm).
package scmmanager

import (
	"fmt"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

// buildValues composes the helm values map. The output is deterministic:
// the same input produces the same map every time, which is essential for
// helm diff stability and for the unit tests.
func buildValues(cfg *config.Config) map[string]any {
	v := map[string]any{
		"persistence": map[string]any{
			"size": "1Gi",
		},
		// Increased start-up time for slower devices – matches the
		// comment in the .ftl.
		"livenessProbe": map[string]any{
			"initialDelaySeconds": 120,
		},
		"fullnameOverride": releaseName,
		"extraEnv":         extraEnvBlock(cfg),
		"service": map[string]any{
			"type": "NodePort",
		},
	}

	if host := cfg.Scm.ScmManager.Ingress; host != "" {
		ingress := map[string]any{
			"enabled": true,
			"path":    "/",
			"hosts":   []any{host},
		}
		if cfg.Features.CertManager.Active {
			ingress["annotations"] = map[string]any{
				"cert-manager.io/cluster-issuer": cfg.Features.CertManager.Issuer,
			}
			ingress["tls"] = []any{
				map[string]any{
					"secretName": "scm-manager-tls",
					"hosts":      []any{host},
				},
			}
		}
		v["ingress"] = ingress
	}

	return v
}

// extraEnvBlock mirrors the multi-line YAML block:
//
//	extraEnv: |
//	  - name: SCM_WEBAPP_INITIALUSER
//	    value: "<username>"
//	  - name: SCM_WEBAPP_INITIALPASSWORD
//	    value: "<password>"
//
// We emit it as a string (rather than a structured list) because the
// upstream chart's values.yaml documents extraEnv as a multi-line string
// that gets concatenated into the deployment's env block verbatim.
func extraEnvBlock(cfg *config.Config) string {
	user := cfg.Scm.ScmManager.Username
	if user == "" {
		user = cfg.Application.Username
	}
	pass := cfg.Scm.ScmManager.Password
	if pass == "" {
		pass = cfg.Application.Password
	}
	return fmt.Sprintf(
		"- name: SCM_WEBAPP_INITIALUSER\n  value: \"%s\"\n- name: SCM_WEBAPP_INITIALPASSWORD\n  value: \"%s\"\n",
		user, pass,
	)
}
