package feature

import (
	"context"
	"fmt"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

// ImagePullSecretCreator is the small abstraction features need to ensure
// their namespace has the "proxy-registry" pull secret. The runner wires
// it to the K8s client at startup; tests can pass a stub.
type ImagePullSecretCreator interface {
	EnsureNamespace(ctx context.Context, name string) error
	CreateImagePullSecret(ctx context.Context, name, namespace, registryURL, user, password string) error
}

// EnsureProxyRegistryPullSecret mirrors ToolWithImage.createImagePullSecret.
// Returns nil when CreateImagePullSecrets is disabled.
func EnsureProxyRegistryPullSecret(ctx context.Context, k ImagePullSecretCreator, cfg *config.Config, namespace string) error {
	r := cfg.Registry
	if !r.CreateImagePullSecrets {
		return nil
	}
	url := r.ProxyURL
	if url == "" {
		url = r.URL
	}
	user := firstNonEmpty(r.ProxyUsername, r.ReadOnlyUsername, r.Username)
	pass := firstNonEmpty(r.ProxyPassword, r.ReadOnlyPassword, r.Password)
	if err := k.EnsureNamespace(ctx, namespace); err != nil {
		return fmt.Errorf("ensure namespace %s: %w", namespace, err)
	}
	if err := k.CreateImagePullSecret(ctx, "proxy-registry", namespace, url, user, pass); err != nil {
		return fmt.Errorf("create proxy-registry pull secret: %w", err)
	}
	return nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
