// Package vault is the Go port of com.cloudogu.gitops.tools.Vault.
//
// The Groovy original renders a Freemarker .ftl helm-values file. Here we
// compose the same values programmatically, which avoids carrying the
// .ftl→.tmpl conversion through every release of the chart and makes
// every value its own testable expression.
//
// Two operational modes are supported via cfg.Features.Secrets.Vault.Mode:
//
//   - "dev"  – Vault runs in-memory, starts unsealed with a deterministic
//     root token, and a post-start ConfigMap script bootstraps users, the
//     kubernetes auth backend and (when ArgoCD is active) per-stage roles.
//   - "prod" – No dev block; the chart is deployed with persistence and the
//     user is expected to initialize/unseal Vault out of band.
package vault

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
	"github.com/cloudogu/gitops-playground/go/internal/feature"
)

const (
	releaseName = "vault"
	repoName    = "vault"
	defaultNS   = "secrets"

	// ConfigMap and volume names used by the dev-mode post-start hook.
	// Kept as constants so tests can assert on them and the future K8s
	// post-start ConfigMap creator can reuse them.
	postStartConfigMap = "vault-dev-post-start"
	postStartVolume    = "dev-post-start"
	postStartScript    = "dev-post-start.sh"
)

// ModeDev / ModeProd are the two recognized values of Vault.Mode.
const (
	ModeDev  = "dev"
	ModeProd = "prod"
)

// Feature implements feature.Feature for Hashicorp Vault.
type Feature struct {
	Deploy deployment.Strategy
	Images feature.ImagePullSecretCreator

	// RootTokenFn is an optional override used by tests so that the
	// generated dev-mode root token is deterministic. Production code
	// leaves it nil and a crypto/rand-backed UUID-ish hex value is used.
	RootTokenFn func() string
}

// Name implements feature.Feature.
func (Feature) Name() string { return "vault" }

// Order is 110 – Vault must come up before ESO (120) so the ESO
// SecretStore can reach it.
func (Feature) Order() int { return 110 }

// IsEnabled implements feature.Feature.
//
// The Groovy gate was secrets.active; here we use the more specific
// Vault.Mode field: any non-empty mode means the user wants Vault.
func (Feature) IsEnabled(cfg *config.Config) bool {
	return cfg.Features.Secrets.Vault.Mode != ""
}

// Namespace returns "<namePrefix>secrets". Both Vault and ESO share this
// namespace; the runner de-duplicates namespace ensures.
func (Feature) Namespace(cfg *config.Config) string {
	return cfg.Application.NamePrefix + defaultNS
}

// Disable is a no-op – Vault is left running so secret data survives a
// reconfigure. Destructive teardown is handled by the destroy command.
func (Feature) Disable(_ context.Context, _ *config.Config) error { return nil }

// Install installs (or upgrades) the Vault chart with values composed by
// buildValues. Dev-mode bootstrapping (the ConfigMap that holds the
// post-start script) is delegated to the runner via the post-install
// hook surface so this package stays Helm-only.
func (f Feature) Install(ctx context.Context, cfg *config.Config) error {
	ns := f.Namespace(cfg)

	if f.Images != nil {
		if err := feature.EnsureProxyRegistryPullSecret(ctx, f.Images, cfg, ns); err != nil {
			return err
		}
	}

	values := f.buildValues(cfg)
	valuesPath, cleanup, err := deployment.RenderHelmValues(cfg, deployment.HelmValuesRequest{
		InlineValues: cfg.Features.Secrets.Vault.Helm.Values,
		ExtraValues:  values,
	})
	if err != nil {
		return fmt.Errorf("vault: render values: %w", err)
	}
	defer cleanup()

	return f.Deploy.Deploy(ctx, deployment.Spec{
		RepoURL:        cfg.Features.Secrets.Vault.Helm.RepoURL,
		RepoName:       repoName,
		ChartOrPath:    cfg.Features.Secrets.Vault.Helm.Chart,
		Version:        cfg.Features.Secrets.Vault.Helm.Version,
		Namespace:      ns,
		ReleaseName:    releaseName,
		HelmValuesPath: valuesPath,
		RepoType:       deployment.RepoHelm,
	})
}

// buildValues is the programmatic Go counterpart of
// vault/templates/values.ftl.yaml. Keep field order stable for
// diffability.
//
// The .ftl branches translate as follows:
//
//	always                                                  → ui, injector
//	<#if config.registry.createImagePullSecrets == true>    → global.imagePullSecrets
//	<#if helm.image?has_content>                            → server.image
//	<#if host?has_content>                                  → server.ingress[+ certManager tls block]
//	<#if dev?has_content>                                   → server.dev / volumes / postStart
//	<#if config.application.podResources == true>           → server.resources
func (f Feature) buildValues(cfg *config.Config) map[string]any {
	v := map[string]any{
		"ui": map[string]any{
			"enabled":      true,
			"externalPort": 80,
			"serviceType":  "ClusterIP",
		},
		"injector": map[string]any{
			"enabled": false,
		},
	}

	if cfg.Registry.CreateImagePullSecrets {
		v["global"] = map[string]any{
			"imagePullSecrets": []any{
				map[string]any{"name": "proxy-registry"},
			},
		}
	}

	server := map[string]any{}

	if img := cfg.Features.Secrets.Vault.Helm.Image; img != "" {
		repo, tag := parseImage(img)
		server["image"] = map[string]any{
			"repository": repo,
			"tag":        tag,
		}
	}

	host := hostFromURL(cfg.Features.Secrets.Vault.URL)
	if host != "" {
		ingress := map[string]any{
			"enabled": true,
			"hosts": []any{
				map[string]any{"host": host},
			},
		}
		if cfg.Features.CertManager.Active {
			ingress["annotations"] = map[string]any{
				"cert-manager.io/cluster-issuer": cfg.Features.CertManager.Issuer,
			}
			ingress["tls"] = []any{
				map[string]any{
					"secretName": "vault-tls",
					"hosts":      []any{host},
				},
			}
		}
		server["ingress"] = ingress
	}

	if cfg.Features.Secrets.Vault.Mode == ModeDev {
		rootToken := f.rootToken()
		server["dev"] = map[string]any{
			"enabled":      true,
			"devRootToken": rootToken,
		}
		server["volumes"] = []any{
			map[string]any{
				"name": postStartVolume,
				"configMap": map[string]any{
					"name":        postStartConfigMap,
					"defaultMode": 0o774,
				},
			},
		}
		server["volumeMounts"] = []any{
			map[string]any{
				"mountPath": "/var/opt/scripts",
				"name":      postStartVolume,
				"readOnly":  true,
			},
		}
		server["postStart"] = []any{
			"/bin/sh",
			"-c",
			fmt.Sprintf(
				"USERNAME=%s PASSWORD=%s ARGOCD=%s /var/opt/scripts/%s 2>&1 | tee /tmp/dev-post-start.log",
				cfg.Application.Username,
				cfg.Application.Password,
				boolToFreemarkerC(cfg.Features.ArgoCD.Active),
				postStartScript,
			),
		}
	}

	if cfg.Application.PodResources {
		server["resources"] = map[string]any{
			"limits":   map[string]any{"memory": "200Mi", "cpu": "500m"},
			"requests": map[string]any{"memory": "100Mi", "cpu": "50m"},
		}
	}

	// The .ftl only emits `server:` when at least one nested field exists.
	// Mirror that to keep generated YAML diff-stable across modes.
	if len(server) > 0 {
		v["server"] = server
	}

	return v
}

// rootToken returns the dev-mode root token. Tests can inject a
// deterministic value via RootTokenFn; otherwise a 32-hex random token is
// generated. We avoid pulling in a UUID library just for this.
func (f Feature) rootToken() string {
	if f.RootTokenFn != nil {
		return f.RootTokenFn()
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is exceptional; fall back to a constant
		// rather than panicking so install can continue (the token is
		// only useful in dev).
		return "dev-only-fallback-token"
	}
	return hex.EncodeToString(b[:])
}

// hostFromURL extracts the hostname from a Vault URL the way the Groovy
// `new URL(url).host` call does. Empty input returns "".
func hostFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// boolToFreemarkerC mirrors Freemarker's `?c` formatter for booleans:
// true → "true", false → "false". Used so the dev-post-start script sees
// a value compatible with the shell guard `if [ "$ARGOCD" = 'true' ]`.
func boolToFreemarkerC(b bool) string {
	if b {
		return "true"
	}
	return "false"
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
