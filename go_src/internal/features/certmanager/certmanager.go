// Package certmanager is the Go port of com.cloudogu.gitops.tools.CertManager.
//
// The Groovy original renders a Freemarker .ftl helm-values file. Here we
// compose the same values programmatically, which avoids carrying the
// .ftl→.tmpl conversion through every release of the chart and makes
// every value its own testable expression.
package certmanager

import (
	"context"
	"fmt"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
	"github.com/cloudogu/gitops-playground/go/internal/feature"
)

const (
	releaseName = "cert-manager"
	repoName    = "cert-manager"
	defaultNS   = "cert-manager"
)

// Feature implements feature.Feature for cert-manager.
type Feature struct {
	Deploy deployment.Strategy
	Images feature.ImagePullSecretCreator
}

// Name implements feature.Feature.
func (Feature) Name() string { return "cert-manager" }

// Order matches the Groovy @Order(160).
func (Feature) Order() int { return 160 }

// IsEnabled implements feature.Feature.
func (Feature) IsEnabled(cfg *config.Config) bool { return cfg.Features.CertManager.Active }

// Namespace returns "<namePrefix>cert-manager", matching the Groovy class.
func (Feature) Namespace(cfg *config.Config) string {
	return cfg.Application.NamePrefix + defaultNS
}

// Disable is a no-op for the imperatively-installed cert-manager.
func (Feature) Disable(_ context.Context, _ *config.Config) error { return nil }

// Install installs (or upgrades) the cert-manager chart with the values
// composed by buildValues.
func (f Feature) Install(ctx context.Context, cfg *config.Config) error {
	ns := f.Namespace(cfg)

	if f.Images != nil {
		if err := feature.EnsureProxyRegistryPullSecret(ctx, f.Images, cfg, ns); err != nil {
			return err
		}
	}

	values := buildValues(cfg)
	valuesPath, cleanup, err := deployment.RenderHelmValues(cfg, deployment.HelmValuesRequest{
		InlineValues: cfg.Features.CertManager.Helm.Values,
		ExtraValues:  values,
	})
	if err != nil {
		return fmt.Errorf("cert-manager: render values: %w", err)
	}
	defer cleanup()

	return f.Deploy.Deploy(ctx, deployment.Spec{
		RepoURL:        cfg.Features.CertManager.Helm.RepoURL,
		RepoName:       repoName,
		ChartOrPath:    cfg.Features.CertManager.Helm.Chart,
		Version:        cfg.Features.CertManager.Helm.Version,
		Namespace:      ns,
		ReleaseName:    releaseName,
		HelmValuesPath: valuesPath,
		RepoType:       deployment.RepoHelm,
	})
}

// buildValues is the programmatic Go counterpart of
// certManager-helm-values.ftl.yaml. Keep field order stable for diffability.
//
// The .ftl branches translate as follows:
//
//	<#if config.registry.createImagePullSecrets == true>   → global.imagePullSecrets
//	always                                                → ingressShim
//	<#if config.application.podResources == true>          → top-level resources
//	<#if config.application.skipCrds != true>             → crds.enabled
//	<#if helm.image?has_content>                          → image (parsed)
//	per-component (webhook, cainjector, acmesolver, startupapicheck):
//	    block exists if podResources OR <componentImage> is set
//	    inner resources only if podResources
//	    inner image only if <componentImage> is set
func buildValues(cfg *config.Config) map[string]any {
	cm := cfg.Features.CertManager
	app := cfg.Application

	v := map[string]any{
		"ingressShim": map[string]any{
			"defaultIssuerName":  cm.Issuer,
			"defaultIssuerKind":  "ClusterIssuer",
			"defaultIssuerGroup": "cert-manager.io",
		},
	}

	if cfg.Registry.CreateImagePullSecrets {
		v["global"] = map[string]any{
			"imagePullSecrets": []any{
				map[string]any{"name": "proxy-registry"},
			},
		}
	}

	if app.PodResources {
		v["resources"] = controllerResources()
	}

	// Groovy uses `!= true`, which in Freemarker treats missing as not-true,
	// i.e. CRDs are installed by default. SkipCRDs is the explicit opt-out.
	if !app.SkipCRDs {
		v["crds"] = map[string]any{"enabled": true}
	}

	if cm.Helm.Image != "" {
		repo, tag := parseImage(cm.Helm.Image)
		v["image"] = map[string]any{
			"repository": repo,
			"tag":        tag,
		}
	}

	if block := componentBlock(app.PodResources, cm.Helm.WebhookImage, webhookResources()); block != nil {
		v["webhook"] = block
	}
	if block := componentBlock(app.PodResources, cm.Helm.CAInjectorImage, controllerResources()); block != nil {
		v["cainjector"] = block
	}
	if block := componentBlock(app.PodResources, cm.Helm.AcmeSolverImage, controllerResources()); block != nil {
		v["acmesolver"] = block
	}
	if block := componentBlock(app.PodResources, cm.Helm.StartupAPICheckImage, controllerResources()); block != nil {
		v["startupapicheck"] = block
	}

	return v
}

// componentBlock returns the {resources?, image?} block for a sub-chart
// component, or nil when neither branch in the .ftl would fire.
func componentBlock(podResources bool, image string, resources map[string]any) map[string]any {
	if !podResources && image == "" {
		return nil
	}
	out := map[string]any{}
	if podResources {
		out["resources"] = resources
	}
	if image != "" {
		repo, tag := parseImage(image)
		out["image"] = map[string]any{
			"repository": repo,
			"tag":        tag,
		}
	}
	return out
}

// controllerResources mirrors the top-level / cainjector / acmesolver /
// startupapicheck resource block from the .ftl (all four are identical).
func controllerResources() map[string]any {
	return map[string]any{
		"limits":   map[string]any{"cpu": "1", "memory": "400Mi"},
		"requests": map[string]any{"cpu": "30m", "memory": "400Mi"},
	}
}

// webhookResources mirrors the smaller webhook block from the .ftl.
func webhookResources() map[string]any {
	return map[string]any{
		"limits":   map[string]any{"cpu": "1", "memory": "30Mi"},
		"requests": map[string]any{"cpu": "20m", "memory": "30Mi"},
	}
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
