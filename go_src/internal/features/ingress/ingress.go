// Package ingress is the Go port of com.cloudogu.gitops.tools.Ingress.
//
// The Groovy original renders a Freemarker .ftl helm-values file. Here we
// compose the same values programmatically, which avoids carrying the
// .ftl→.tmpl conversion through every release of the chart and makes
// every value its own testable expression.
package ingress

import (
	"context"
	"fmt"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
	"github.com/cloudogu/gitops-playground/go/internal/feature"
)

const (
	releaseName = "traefik"
	repoName    = "traefik"
	defaultNS   = "ingress"
)

// Feature implements feature.Feature for the Traefik ingress controller.
type Feature struct {
	Deploy      deployment.Strategy
	Images      feature.ImagePullSecretCreator
}

// Name implements feature.Feature.
func (Feature) Name() string { return "ingress" }

// Order matches the Groovy @Order(150).
func (Feature) Order() int { return 150 }

// IsEnabled implements feature.Feature.
func (Feature) IsEnabled(cfg *config.Config) bool { return cfg.Features.Ingress.Active }

// Namespace returns the configured namespace prefixed with the tenant.
func (Feature) Namespace(cfg *config.Config) string {
	ns := cfg.Features.Ingress.IngressNamespace
	if ns == "" {
		ns = defaultNS
	}
	return cfg.Application.NamePrefix + ns
}

// Disable is a no-op for the imperatively-installed ingress.
func (Feature) Disable(_ context.Context, _ *config.Config) error { return nil }

// Install installs (or upgrades) the Traefik chart with the values
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
		InlineValues: cfg.Features.Ingress.Helm.Values,
		ExtraValues:  values,
	})
	if err != nil {
		return fmt.Errorf("ingress: render values: %w", err)
	}
	defer cleanup()

	return f.Deploy.Deploy(ctx, deployment.Spec{
		RepoURL:        cfg.Features.Ingress.Helm.RepoURL,
		RepoName:       repoName,
		ChartOrPath:    cfg.Features.Ingress.Helm.Chart,
		Version:        cfg.Features.Ingress.Helm.Version,
		Namespace:      ns,
		ReleaseName:    releaseName,
		HelmValuesPath: valuesPath,
		RepoType:       deployment.RepoHelm,
	})
}

// buildValues is the programmatic Go counterpart of
// ingress-helm-values.ftl.yaml. Keep field order stable for diffability.
func buildValues(cfg *config.Config) map[string]any {
	v := map[string]any{
		"deployment": map[string]any{
			"kind": "Deployment",
			"podAnnotations": map[string]any{
				"ingressclass.kubernetes.io/is-default-class": "true",
			},
			"podLabels": map[string]any{
				"traefik.http.middlewares.gzip.compress": "true",
			},
			"admissionWebhooks": map[string]any{
				"enabled": false,
			},
			"service": map[string]any{
				"externalTrafficPolicy": "Local",
			},
			"ports": map[string]any{
				"websecure": map[string]any{
					"proxyProtocol":    trustedIPsBlock(),
					"forwardedHeaders": trustedIPsBlock(),
				},
			},
			"replicaCount": 2,
			"resources": map[string]any{
				"limits":   map[string]any{"cpu": "1", "memory": "1Gi"},
				"requests": map[string]any{"cpu": "100m", "memory": "90Mi"},
			},
		},
		"logs": map[string]any{
			"general": map[string]any{
				"level":  "INFO",
				"access": map[string]any{"enabled": true},
			},
		},
		"global": map[string]any{
			"checknewversion":    false,
			"sendAnonymousUsage": false,
		},
		"providers": map[string]any{
			"kubernetesGateway": map[string]any{"enabled": true},
		},
		"gatewayClass": map[string]any{"enabled": true, "name": "traefik"},
		"gateway":      map[string]any{"enabled": true},
	}

	if img := cfg.Features.Ingress.Helm.Image; img != "" {
		repo, tag := parseImage(img)
		v["image"] = map[string]any{
			"repository": repo,
			"tag":        tag,
			"digest":     nil,
		}
	}
	if cfg.Registry.CreateImagePullSecrets {
		v["deployment"].(map[string]any)["imagePullSecrets"] = []any{
			map[string]any{"name": "proxy-registry"},
		}
	}
	if cfg.Application.NetPols {
		v["deployment"].(map[string]any)["networkPolicy"] = map[string]any{"enabled": true}
	}
	if cfg.Features.Monitoring.Active {
		v["metrics"] = map[string]any{
			"enabled": true,
			"prometheus": map[string]any{
				"enabled": true,
				"service": map[string]any{"enabled": true},
				"serviceMonitor": map[string]any{
					"enabled":          true,
					"namespace":        cfg.Application.NamePrefix + "monitoring",
					"additionalLabels": map[string]any{"release": "kube-prometheus-stack"},
				},
			},
		}
	}
	return v
}

func trustedIPsBlock() map[string]any {
	return map[string]any{
		"trustedIPs": []any{"127.0.0.1/32", "172.18.0.0/12"},
	}
}

// parseImage splits a docker reference into "registry/repo" and "tag".
// "image:tag" → ("image", "tag"); "image" → ("image", "latest").
// Ports embedded in the registry (e.g. localhost:5000/repo:tag) are kept
// with the registry segment intact.
func parseImage(ref string) (string, string) {
	// Find the last ":". A ":" before the last "/" is a port, not a tag.
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
