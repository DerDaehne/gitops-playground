// Package externalsecrets is the Go port of
// com.cloudogu.gitops.tools.ExternalSecretsOperator.
//
// The Groovy original renders a Freemarker .ftl helm-values file. Here we
// compose the same values programmatically, which avoids carrying the
// .ftl→.tmpl conversion through every release of the chart and makes
// every value its own testable expression.
//
// ESO shares its namespace with Vault ("<namePrefix>secrets") because the
// SecretStore CR that bridges the two lives in that namespace. The Feature
// runner de-duplicates namespace ensures, so co-locating them is safe.
package externalsecrets

import (
	"context"
	"fmt"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
	"github.com/cloudogu/gitops-playground/go/internal/feature"
)

const (
	releaseName = "external-secrets-operator"
	repoName    = "external-secrets"
	defaultNS   = "secrets"
)

// Feature implements feature.Feature for the External Secrets Operator.
type Feature struct {
	Deploy deployment.Strategy
	Images feature.ImagePullSecretCreator
}

// Name implements feature.Feature.
func (Feature) Name() string { return "external-secrets" }

// Order is 120 – ESO is installed after Vault (110) so the SecretStore
// it consumes already has a reachable backend.
func (Feature) Order() int { return 120 }

// IsEnabled implements feature.Feature.
//
// Matches the Groovy tool: ESO is gated by the top-level secrets.active
// flag (cfg.Features.Secrets.Active).
func (Feature) IsEnabled(cfg *config.Config) bool { return cfg.Features.Secrets.Active }

// Namespace returns "<namePrefix>secrets". Shared with Vault.
func (Feature) Namespace(cfg *config.Config) string {
	return cfg.Application.NamePrefix + defaultNS
}

// Disable is a no-op – ESO custom resources may still be in use by other
// tenants, so we don't tear them down implicitly.
func (Feature) Disable(_ context.Context, _ *config.Config) error { return nil }

// Install installs (or upgrades) the External Secrets Operator chart with
// values composed by buildValues.
func (f Feature) Install(ctx context.Context, cfg *config.Config) error {
	ns := f.Namespace(cfg)

	if f.Images != nil {
		if err := feature.EnsureProxyRegistryPullSecret(ctx, f.Images, cfg, ns); err != nil {
			return err
		}
	}

	values := buildValues(cfg)
	valuesPath, cleanup, err := deployment.RenderHelmValues(cfg, deployment.HelmValuesRequest{
		InlineValues: cfg.Features.Secrets.ExternalSecrets.Helm.Values,
		ExtraValues:  values,
	})
	if err != nil {
		return fmt.Errorf("external-secrets: render values: %w", err)
	}
	defer cleanup()

	return f.Deploy.Deploy(ctx, deployment.Spec{
		RepoURL:        cfg.Features.Secrets.ExternalSecrets.Helm.RepoURL,
		RepoName:       repoName,
		ChartOrPath:    cfg.Features.Secrets.ExternalSecrets.Helm.Chart,
		Version:        cfg.Features.Secrets.ExternalSecrets.Helm.Version,
		Namespace:      ns,
		ReleaseName:    releaseName,
		HelmValuesPath: valuesPath,
		RepoType:       deployment.RepoHelm,
	})
}

// buildValues is the programmatic Go counterpart of
// external-secrets/templates/values.ftl.yaml. Keep field order stable for
// diffability.
//
// The .ftl branches translate as follows:
//
//	<#if config.application.skipCrds == true>           → installCRDs: false
//	<#if config.application.podResources == true>       → certController.resources,
//	                                                       webhook.resources,
//	                                                       top-level resources
//	<#if config.registry.createImagePullSecrets == true> → imagePullSecrets
//	<#if helm.image?has_content>                         → image
//	<#if helm.certControllerImage?has_content>           → certController.image
//	    nested:
//	    <#if config.registry.createImagePullSecrets>     → certController.imagePullSecrets
//	<#if helm.webhookImage?has_content>                  → webhook.image
//	    nested:
//	    <#if config.registry.createImagePullSecrets>     → webhook.imagePullSecrets
//
// Tricky bit: the Freemarker template uses `== true` for skipCrds, so a
// missing value behaves as "install CRDs". We mirror that exact polarity
// to avoid silently flipping the default for existing users.
func buildValues(cfg *config.Config) map[string]any {
	eso := cfg.Features.Secrets.ExternalSecrets
	app := cfg.Application
	reg := cfg.Registry

	v := map[string]any{}

	if app.SkipCRDs {
		v["installCRDs"] = false
	}

	// certController and webhook get separate sub-blocks because the
	// .ftl template may add resources from podResources AND/OR an
	// image from the per-component image config; both contribute to the
	// same key.
	certController := map[string]any{}
	webhook := map[string]any{}

	if app.PodResources {
		certController["resources"] = map[string]any{
			"limits":   map[string]any{"memory": "110Mi", "cpu": "500m"},
			"requests": map[string]any{"memory": "55Mi", "cpu": "50m"},
		}
		webhook["resources"] = map[string]any{
			"limits":   map[string]any{"memory": "50Mi", "cpu": "500m"},
			"requests": map[string]any{"memory": "25Mi", "cpu": "50m"},
		}
		v["resources"] = map[string]any{
			"limits":   map[string]any{"memory": "80Mi", "cpu": "500m"},
			"requests": map[string]any{"memory": "40Mi", "cpu": "50m"},
		}
	}

	if reg.CreateImagePullSecrets {
		v["imagePullSecrets"] = []any{
			map[string]any{"name": "proxy-registry"},
		}
	}

	if img := eso.Helm.Image; img != "" {
		repo, tag := parseImage(img)
		v["image"] = map[string]any{
			"repository": repo,
			"tag":        tag,
		}
	}

	if img := eso.Helm.CertControllerImage; img != "" {
		repo, tag := parseImage(img)
		certController["image"] = map[string]any{
			"repository": repo,
			"tag":        tag,
		}
		if reg.CreateImagePullSecrets {
			certController["imagePullSecrets"] = []any{
				map[string]any{"name": "proxy-registry"},
			}
		}
	}

	if img := eso.Helm.WebhookImage; img != "" {
		repo, tag := parseImage(img)
		webhook["image"] = map[string]any{
			"repository": repo,
			"tag":        tag,
		}
		if reg.CreateImagePullSecrets {
			webhook["imagePullSecrets"] = []any{
				map[string]any{"name": "proxy-registry"},
			}
		}
	}

	// The .ftl only emits the certController / webhook block when at
	// least one nested field exists; mirror that so unrelated values
	// don't shift in the rendered YAML.
	if len(certController) > 0 {
		v["certController"] = certController
	}
	if len(webhook) > 0 {
		v["webhook"] = webhook
	}

	return v
}

// parseImage splits a docker reference into "registry/repo" and "tag".
// "image:tag" → ("image", "tag"); "image" → ("image", "latest").
// Ports embedded in the registry (e.g. localhost:5000/repo:tag) are kept
// with the registry segment intact.
func parseImage(ref string) (string, string) {
	lastSlash := -1
	for i := 0; i < len(ref); i++ {
		if ref[i] == '/' {
			lastSlash = i
		}
	}
	for i := len(ref) - 1; i > lastSlash; i-- {
		if ref[i] == ':' {
			return ref[:i], ref[i+1:]
		}
	}
	return ref, "latest"
}
